package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// Onboarding step ids. The first-run guide is UI guidance only: it does not
// create templates, contracts or sample records. Steps backed by real data are
// derived from the database on every read; the two sample-only steps are
// recorded in config.json because they intentionally leave no business data.
const (
	OnboardingStepTemplateImport    = "template_import"
	OnboardingStepTemplateCalibrate = "template_calibrate"
	OnboardingStepSamplePreview     = "sample_preview"
	OnboardingStepTrialPDF          = "trial_pdf"
	OnboardingStepContractCreate    = "contract_create"
)

// OnboardingFacts summarizes the data the guide derives its step state from.
type OnboardingFacts struct {
	Templates                int    `json:"templates"`
	CalibratedTemplates      int    `json:"calibrated_templates"`
	Contracts                int    `json:"contracts"`
	Parties                  int    `json:"parties"`
	Fields                   int    `json:"fields"`
	ActiveTemplateID         string `json:"active_template_id,omitempty"`
	ActiveTemplateName       string `json:"active_template_name,omitempty"`
	ActiveTemplateVersion    int    `json:"active_template_version,omitempty"`
	ActiveTemplatePlacements int    `json:"active_template_placements"`
}

// OnboardingFacts reads the counts and the active PDF template used by the
// first-run guide. It never writes anything.
func (s *Service) OnboardingFacts(ctx context.Context) (OnboardingFacts, error) {
	facts := OnboardingFacts{}
	templates, err := s.rd.ListTemplates(ctx, model.ListFilter{Limit: 500, Kind: model.TemplateKindPDF})
	if err != nil {
		return facts, err
	}
	facts.Templates = templates.Total
	active := model.Template{}
	for _, tpl := range templates.Items {
		if !tpl.IsActive {
			continue
		}
		// A template only counts as calibrated once its current (active)
		// version has positions; an old calibrated version replaced by an
		// empty import is not a usable starting point.
		if len(tpl.Placements) > 0 {
			facts.CalibratedTemplates++
		}
		if active.ID == "" || tpl.UpdatedAt.After(active.UpdatedAt) {
			active = tpl
		}
	}
	if active.ID != "" {
		facts.ActiveTemplateID = active.ID
		facts.ActiveTemplateName = active.Name
		facts.ActiveTemplateVersion = active.Version
		facts.ActiveTemplatePlacements = len(active.Placements)
	}
	contracts, err := s.rd.ListContracts(ctx, model.ListFilter{Limit: 1})
	if err != nil {
		return facts, err
	}
	facts.Contracts = contracts.Total
	stats, err := s.rd.Stats(ctx)
	if err != nil {
		return facts, err
	}
	facts.Parties = stats.Parties
	facts.Fields = stats.Fields
	return facts, nil
}

// Self-check statuses. Each item is a single line in the first-run self-check
// page; non-ok items must carry an actionable next step.
const (
	CheckStatusOK    = "ok"
	CheckStatusWarn  = "warn"
	CheckStatusError = "error"
)

// OnboardingCheckInput carries the HTTP-layer facts the service cannot read
// itself (the startup listen configuration).
type OnboardingCheckInput struct {
	AllowLAN bool
	LANHosts []string
}

// OnboardingCheckItem is one read-only self-check result.
type OnboardingCheckItem struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Action string `json:"action,omitempty"`
	// Target is the app tab that fixes the issue ("" when the fix is outside
	// the app, such as installing LibreOffice).
	Target string `json:"target,omitempty"`
}

// OnboardingCheck runs the read-only self-check. Every probe reports its own
// status; a broken probe never hides the other results.
func (s *Service) OnboardingCheck(ctx context.Context, in OnboardingCheckInput) []OnboardingCheckItem {
	items := []OnboardingCheckItem{
		s.checkDataDir(),
		s.checkDatabase(ctx),
		s.checkConverters(),
		s.checkSignatureLAN(in),
		s.checkFields(ctx),
		s.checkTemplate(ctx),
		s.checkSample(ctx),
		s.checkBackup(ctx),
		s.checkDisk(),
	}
	return items
}

func (s *Service) checkDataDir() OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "data_dir", Label: "数据目录可写", Target: "data"}
	dir := strings.TrimSpace(s.dataDir)
	if dir == "" {
		item.Status = CheckStatusError
		item.Detail = "数据目录未配置"
		item.Action = "以桌面应用启动，或用 tengshou-cli serve --data-dir <目录> 指定数据目录"
		return item
	}
	f, err := os.CreateTemp(dir, ".onboarding-check-*")
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "无法在数据目录中创建文件：" + compactError(err)
		item.Action = "检查数据目录权限（当前用户需要读写权限），或把数据目录换到可写位置"
		return item
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	item.Status = CheckStatusOK
	item.Detail = dir
	return item
}

func (s *Service) checkDatabase(ctx context.Context) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "database", Label: "数据库版本与完整性", Target: "data"}
	version, err := s.rd.SchemaVersion(ctx)
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "读取数据库版本失败：" + compactError(err)
		item.Action = "重新打开腾手；仍失败请在「数据安全」页恢复备份"
		return item
	}
	latest := store.LatestSchemaVersion()
	if err := s.st.IntegrityCheck(); err != nil {
		item.Status = CheckStatusError
		item.Detail = "完整性检查失败：" + compactError(err)
		item.Action = "先关闭腾手并从备份包恢复；没有备份时请在「数据安全」页保留现有数据目录再排查"
		return item
	}
	if version != latest {
		item.Status = CheckStatusError
		item.Detail = fmt.Sprintf("数据库 schema v%d，当前程序需要 v%d", version, latest)
		item.Action = "重新打开腾手以完成数据库升级；升级前建议先导出备份"
		return item
	}
	item.Status = CheckStatusOK
	item.Detail = fmt.Sprintf("schema v%d，完整性检查通过", version)
	return item
}

// checkConverters reports whether Word documents can be converted to PDF.
// Windows uses Microsoft Word through COM; other platforms use LibreOffice.
// Importing a PDF directly never depends on either program.
func (s *Service) checkConverters() OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "converter", Label: "Word 转 PDF（模板导入）", Target: "templates"}
	list := s.Converters()
	available := false
	parts := make([]string, 0, len(list))
	for _, conv := range list {
		label := conv.Label
		if conv.Name == "word" {
			label = "Microsoft Word（COM）"
		}
		state := "不可用：" + conv.Detail
		if conv.Available {
			state = "可用：" + conv.Detail
			available = true
		}
		parts = append(parts, label+"（"+state+"）")
	}
	item.Detail = strings.Join(parts, "；") + "。也可以直接用 Word / WPS 另存为 PDF 后导入 PDF，不需要转换程序。"
	if !available {
		item.Status = CheckStatusWarn
		item.Action = "安装 LibreOffice（免费），或用 Word / WPS 把模板另存为 PDF 后直接导入 PDF"
		return item
	}
	item.Status = CheckStatusOK
	return item
}

func (s *Service) checkSignatureLAN(in OnboardingCheckInput) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "signature_lan", Label: "局域网扫码签名", Target: "settings"}
	switch {
	case !in.AllowLAN:
		item.Status = CheckStatusWarn
		item.Detail = "局域网签名已关闭（config.json 的 http.allow_lan=false）"
		item.Action = "如需手机扫码留存签名，把 allow_lan 改为 true 后重新打开腾手；不用手机签名可忽略"
	case len(in.LANHosts) == 0:
		item.Status = CheckStatusWarn
		item.Detail = "已允许局域网，但没有检测到可用的局域网地址"
		item.Action = "连接局域网（Wi-Fi 或网线）后重新检查"
	default:
		hosts := append([]string(nil), in.LANHosts...)
		sort.Strings(hosts)
		item.Status = CheckStatusOK
		item.Detail = "局域网地址：" + strings.Join(hosts, "、")
	}
	return item
}

func (s *Service) checkFields(ctx context.Context) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "fields", Label: "字段定义就绪", Target: "fields"}
	contractFields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "读取合同字段失败：" + compactError(err)
		item.Action = "重新打开腾手后再试"
		return item
	}
	partyFields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityParty, false)
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "读取相对方字段失败：" + compactError(err)
		item.Action = "重新打开腾手后再试"
		return item
	}
	item.Status = CheckStatusOK
	item.Detail = fmt.Sprintf("合同字段 %d 个，相对方字段 %d 个", len(contractFields), len(partyFields))
	return item
}

func (s *Service) checkTemplate(ctx context.Context) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "template", Label: "模板与标定就绪", Target: "templates"}
	facts, err := s.OnboardingFacts(ctx)
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "读取模板状态失败：" + compactError(err)
		item.Action = "重新打开腾手后再试"
		return item
	}
	switch {
	case facts.Templates == 0:
		item.Status = CheckStatusWarn
		item.Detail = "还没有导入模板"
		item.Action = "打开「模板」页，导入您收到的 Word 模板（.docx）或现成 PDF"
	case facts.CalibratedTemplates == 0:
		item.Status = CheckStatusWarn
		item.Detail = fmt.Sprintf("模板「%s」已导入，但当前版本还没有标定位置", facts.ActiveTemplateName)
		item.Action = "打开模板标定页，拖框标出哪里填字段、哪里落签名与公章"
	case facts.ActiveTemplatePlacements > 0:
		item.Status = CheckStatusOK
		item.Detail = fmt.Sprintf("当前模板「%s」v%d 已标定 %d 个位置",
			facts.ActiveTemplateName, facts.ActiveTemplateVersion, facts.ActiveTemplatePlacements)
	default:
		item.Status = CheckStatusOK
		item.Detail = fmt.Sprintf("已有 %d 个模板完成标定", facts.CalibratedTemplates)
	}
	return item
}

func (s *Service) checkSample(ctx context.Context) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "sample", Label: "样例预览与试生成", Target: "templates"}
	facts, err := s.OnboardingFacts(ctx)
	if err != nil {
		item.Status = CheckStatusError
		item.Detail = "读取模板状态失败：" + compactError(err)
		item.Action = "重新打开腾手后再试"
		return item
	}
	if facts.Templates == 0 || facts.CalibratedTemplates == 0 {
		item.Status = CheckStatusWarn
		item.Detail = "导入并标定模板后即可用样例数据预览与试生成"
		item.Action = "先完成模板导入与标定；样例数据不会写入台账"
		return item
	}
	if _, err := s.FontFamily(); err != nil {
		item.Status = CheckStatusError
		item.Detail = "中文字体不可用，套印文字无法生成：" + compactError(err)
		item.Action = "安装中文字体，或在 config.json 的 pdf.font_path 指定字体文件后重新打开腾手"
		return item
	}
	item.Status = CheckStatusOK
	item.Detail = "样例预览与试生成可用；样例数据不写入台账"
	return item
}

func (s *Service) checkBackup(ctx context.Context) OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "backup", Label: "完整备份", Target: "data"}
	if dir := strings.TrimSpace(s.dataDir); dir != "" {
		backups := filepath.Join(dir, "backups")
		entries, err := os.ReadDir(backups)
		if err == nil {
			newest := ""
			var newestInfo os.FileInfo
			for _, entry := range entries {
				if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				switch strings.ToLower(filepath.Ext(entry.Name())) {
				case ".zip", ".db":
				default:
					continue
				}
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if newest == "" || info.ModTime().After(newestInfo.ModTime()) {
					newest, newestInfo = entry.Name(), info
				}
			}
			if newest != "" {
				item.Status = CheckStatusOK
				item.Detail = fmt.Sprintf("最近备份文件：%s（%s）", newest, newestInfo.ModTime().Format("2006-01-02 15:04"))
				return item
			}
		}
	}
	if logs, err := s.rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntityBackup, Limit: 1}); err == nil &&
		len(logs.Items) > 0 && logs.Items[0].Action == model.ChangeActionExport {
		item.Status = CheckStatusOK
		item.Detail = fmt.Sprintf("最近导出备份包：%s；请确认文件已妥善保存",
			logs.Items[0].CreatedAt.Local().Format("2006-01-02 15:04"))
		return item
	}
	item.Status = CheckStatusWarn
	item.Detail = "还没有备份记录"
	item.Action = "打开「数据安全」页导出完整备份包；备份包含数据库、模板、签名、公章与已生成 PDF"
	return item
}

func (s *Service) checkDisk() OnboardingCheckItem {
	item := OnboardingCheckItem{ID: "disk", Label: "磁盘剩余空间", Target: "data"}
	dir := strings.TrimSpace(s.dataDir)
	if dir == "" {
		item.Status = CheckStatusWarn
		item.Detail = "数据目录未配置，无法检查磁盘空间"
		item.Action = "先配置数据目录"
		return item
	}
	free, total, err := diskSpace(dir)
	if err != nil {
		item.Status = CheckStatusWarn
		item.Detail = "读取磁盘空间失败：" + compactError(err)
		item.Action = "确认数据目录所在磁盘可访问"
		return item
	}
	item.Detail = fmt.Sprintf("可用 %s / 共 %s", humanSize(free), humanSize(total))
	const (
		warnBelow  = int64(1) << 30
		errorBelow = int64(200) << 20
	)
	switch {
	case free < errorBelow:
		item.Status = CheckStatusError
		item.Action = "磁盘空间严重不足，请立即清理磁盘或把数据目录移到空间更大的磁盘"
	case free < warnBelow:
		item.Status = CheckStatusWarn
		item.Action = "磁盘空间偏少，建议清理磁盘或把数据目录移到空间更大的磁盘"
	default:
		item.Status = CheckStatusOK
	}
	return item
}

// TemplateCheckIssue is one problem found by the template wizard's validation.
type TemplateCheckIssue struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	PlacementID string `json:"placement_id,omitempty"`
	Field       string `json:"field,omitempty"`
	Message     string `json:"message"`
	Action      string `json:"action"`
}

// TemplateCheck is the read-only template validation shown before the first
// trial generation. It compares calibrated placements with the current field
// definitions; it never changes the template or the mapping.
type TemplateCheck struct {
	TemplateID          string               `json:"template_id"`
	Name                string               `json:"name"`
	Version             int                  `json:"version"`
	PlacementCount      int                  `json:"placement_count"`
	FieldPlacements     int                  `json:"field_placements"`
	SignaturePlacements int                  `json:"signature_placements"`
	SealPlacements      int                  `json:"seal_placements"`
	Issues              []TemplateCheckIssue `json:"issues"`
	OK                  bool                 `json:"ok"`
}

// CheckTemplate validates one PDF template version against the current field
// catalogue. Unknown or archived bindings, unbound required fields and missing
// signature/seal positions are reported with a concrete next step.
func (s *Service) CheckTemplate(ctx context.Context, id string) (TemplateCheck, error) {
	tpl, err := s.GetTemplate(ctx, id)
	if err != nil {
		return TemplateCheck{}, err
	}
	if tpl.Kind != model.TemplateKindPDF {
		return TemplateCheck{}, business("invalid_kind", "只有 PDF 模板可以做占位检查")
	}
	contractFields, err := s.fieldMap(ctx, model.FieldEntityContract)
	if err != nil {
		return TemplateCheck{}, err
	}
	partyFields, err := s.fieldMap(ctx, model.FieldEntityParty)
	if err != nil {
		return TemplateCheck{}, err
	}
	result := TemplateCheck{
		TemplateID:     tpl.ID,
		Name:           tpl.Name,
		Version:        tpl.Version,
		PlacementCount: len(tpl.Placements),
		Issues:         []TemplateCheckIssue{},
	}
	bound := map[string]bool{}
	for _, p := range tpl.Placements {
		switch p.Type {
		case model.PlacementField:
			result.FieldPlacements++
			key := strings.TrimSpace(p.Field)
			if err := validateBinding(key, contractFields, partyFields); err != nil {
				result.Issues = append(result.Issues, TemplateCheckIssue{
					Code:        "unknown_field",
					Severity:    CheckStatusError,
					PlacementID: p.ID,
					Field:       key,
					Message:     fmt.Sprintf("位置 %s 的字段绑定无效：%s", p.ID, err.Error()),
					Action:      "去「字段」页新增该字段，或到模板标定页把该位置改绑到已有字段",
				})
				continue
			}
			bound[key] = true
			if field, ok := bindingField(key, contractFields, partyFields); ok && field.Archived {
				result.Issues = append(result.Issues, TemplateCheckIssue{
					Code:        "archived_field",
					Severity:    CheckStatusWarn,
					PlacementID: p.ID,
					Field:       key,
					Message:     fmt.Sprintf("位置 %s 绑定的字段「%s」已归档，生成时该位置为空", p.ID, field.Label),
					Action:      "到「字段」页恢复该字段，或到模板标定页改绑其他字段",
				})
			}
		case model.PlacementSignature:
			result.SignaturePlacements++
		case model.PlacementSeal:
			result.SealPlacements++
		}
	}
	for _, field := range contractFields {
		if field.Archived || !field.Required || field.Type == model.FieldTypeImage {
			continue
		}
		if !bound[bindingKey("contract", field)] {
			result.Issues = append(result.Issues, TemplateCheckIssue{
				Code:     "missing_required_field",
				Severity: CheckStatusWarn,
				Field:    bindingKey("contract", field),
				Message:  fmt.Sprintf("必填合同字段「%s」还没有标定位置", field.Label),
				Action:   "到模板标定页添加该字段的位置，或确认这个字段不需要出现在合同正文中",
			})
		}
	}
	for _, field := range partyFields {
		if field.Archived || !field.Required || field.Type == model.FieldTypeImage {
			continue
		}
		if !bound[bindingKey("party_a", field)] && !bound[bindingKey("party_b", field)] {
			result.Issues = append(result.Issues, TemplateCheckIssue{
				Code:     "missing_required_party_field",
				Severity: CheckStatusWarn,
				Field:    bindingKey("party_b", field),
				Message:  fmt.Sprintf("必填相对方字段「%s」还没有标定位置", field.Label),
				Action:   "到模板标定页添加该字段位置（甲方或乙方均可），或确认不需要出现在合同正文中",
			})
		}
	}
	if result.PlacementCount == 0 {
		result.Issues = append(result.Issues, TemplateCheckIssue{
			Code:     "no_placements",
			Severity: CheckStatusWarn,
			Message:  "模板还没有任何标定位置",
			Action:   "打开模板标定页，先添加字段位置，再试生成",
		})
	} else if result.FieldPlacements == 0 {
		result.Issues = append(result.Issues, TemplateCheckIssue{
			Code:     "no_field_placements",
			Severity: CheckStatusWarn,
			Message:  "模板还没有字段位置，生成的合同不会填入数据",
			Action:   "打开模板标定页添加字段位置",
		})
	}
	if result.SignaturePlacements == 0 {
		result.Issues = append(result.Issues, TemplateCheckIssue{
			Code:     "no_signature_placement",
			Severity: CheckStatusWarn,
			Message:  "模板没有签名位置，生成的合同不会显示留存签名",
			Action:   "如需在合同上留存手写签名，到模板标定页添加签名位置；不需要可忽略",
		})
	}
	if result.SealPlacements == 0 {
		result.Issues = append(result.Issues, TemplateCheckIssue{
			Code:     "no_seal_placement",
			Severity: CheckStatusWarn,
			Message:  "模板没有公章位置，生成的合同不会落公章图片",
			Action:   "如需在合同上落公章，到模板标定页添加公章位置；不需要可忽略",
		})
	}
	result.OK = true
	for _, issue := range result.Issues {
		if issue.Severity == CheckStatusError {
			result.OK = false
			break
		}
	}
	return result, nil
}

// bindingField resolves a placement binding key to its field definition.
func bindingField(key string, contractFields, partyFields map[string]model.FieldDefinition) (model.FieldDefinition, bool) {
	prefix, rest, ok := strings.Cut(strings.TrimSpace(key), ".")
	if !ok {
		return model.FieldDefinition{}, false
	}
	var fields map[string]model.FieldDefinition
	switch prefix {
	case "contract":
		fields = contractFields
	case "party_a", "party_b":
		fields = partyFields
	default:
		return model.FieldDefinition{}, false
	}
	if dataKey, ok := strings.CutPrefix(rest, "data."); ok {
		rest = dataKey
	}
	field, exists := fields[rest]
	return field, exists
}

// bindingKey returns the canonical placement key for one field definition.
func bindingKey(prefix string, field model.FieldDefinition) string {
	if field.BuiltIn {
		return prefix + "." + field.Key
	}
	return prefix + ".data." + field.Key
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}
