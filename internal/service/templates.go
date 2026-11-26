package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/tdewolff/canvas"

	"github.com/LuSrackhall/tengshou/internal/doctopdf"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/overlay"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
)

const (
	// maxTemplateSourceBytes bounds an imported Word document or PDF.
	maxTemplateSourceBytes = 40 << 20
	// maxPlacements bounds one template version's calibrated boxes.
	maxPlacements = 500
	// conversionTimeout bounds a Word/LibreOffice conversion.
	conversionTimeout = 3 * time.Minute
)

// SetFontLoader injects the CJK font resolver used for overlay text. The app
// layer shares its generator font so loading happens once per process.
func (s *Service) SetFontLoader(loader func() (*canvas.FontFamily, error)) {
	s.fontLoader = loader
	s.fontOnce = sync.Once{}
	s.font, s.fontErr = nil, nil
}

// FontFamily returns the CJK font used for overlays, loading it lazily.
func (s *Service) FontFamily() (*canvas.FontFamily, error) {
	s.fontOnce.Do(func() {
		loader := s.fontLoader
		if loader == nil {
			loader = func() (*canvas.FontFamily, error) {
				family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{
					DataFontsDir: filepath.Join(s.dataDir, "fonts"),
				})
				return family, err
			}
		}
		s.font, s.fontErr = loader()
	})
	if s.fontErr != nil {
		return nil, business("font_unavailable", "找不到中文字体，无法生成套印文字：%s", s.fontErr.Error())
	}
	return s.font, nil
}

// Converters lists docx→PDF converter availability for the UI.
func (s *Service) Converters() []doctopdf.Availability {
	return s.converters.Availability()
}

// TemplateImportInput is one uploaded template file.
type TemplateImportInput struct {
	Name     string `json:"name"`
	FileName string `json:"file_name"`
	Data     []byte `json:"-"`
}

// TemplateSaveInput replaces the calibrated positions of a PDF template,
// producing a new version.
type TemplateSaveInput struct {
	Placements []model.Placement `json:"placements"`
	IsActive   *bool             `json:"is_active"`
	// MaskFieldValues toggles the white background behind field values for the
	// new version. Nil keeps the current template's setting.
	MaskFieldValues *bool `json:"mask_field_values"`
}

// TemplateImportResult is returned after importing or re-saving a template.
type TemplateImportResult struct {
	Template model.Template      `json:"template"`
	Usage    model.TemplateUsage `json:"usage"`
	Warnings []string            `json:"warnings"`
	// Overlaps lists seal/signature boxes covering field boxes beyond the
	// warning threshold; the save still succeeds.
	Overlaps []PlacementOverlap `json:"overlaps,omitempty"`
}

// TemplatePreviewInput selects the data used for a preview.
type TemplatePreviewInput struct {
	ContractID string `json:"contract_id"`
	// Sample uses built-in example values instead of a real contract.
	Sample bool `json:"sample"`
}

// TemplatePreviewResult is a rendered preview (not persisted).
type TemplatePreviewResult struct {
	PDF           []byte            `json:"-"`
	Warnings      []overlay.Warning `json:"warnings"`
	PreviewSHA256 string            `json:"preview_sha256"`
	PageCount     int               `json:"page_count"`
	Sample        bool              `json:"sample"`
	ContractID    string            `json:"contract_id,omitempty"`
}

// TemplateRenderResult is a persisted output document.
type TemplateRenderResult struct {
	Document model.OutputDocument `json:"document"`
	Usage    model.TemplateUsage  `json:"usage"`
	Warnings []overlay.Warning    `json:"warnings"`
}

// ListTemplates is the read surface used by the app API. kind may be "" for
// all templates or model.TemplateKindPDF / model.TemplateKindHTML.
func (s *Service) ListTemplates(ctx context.Context, f model.ListFilter) (model.List[model.Template], error) {
	return s.rd.ListTemplates(ctx, f)
}

// GetTemplate returns one template with its calibrated placements.
func (s *Service) GetTemplate(ctx context.Context, id string) (model.Template, error) {
	tpl, err := s.rd.GetTemplate(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.Template{}, business("not_found", "模板不存在")
		}
		return model.Template{}, err
	}
	return tpl, nil
}

// TemplateUsage reports how many contracts and PDFs use a template family.
func (s *Service) TemplateUsage(ctx context.Context, id string) (model.TemplateUsage, error) {
	usage, err := s.rd.TemplateUsage(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.TemplateUsage{}, business("not_found", "模板不存在")
		}
		return model.TemplateUsage{}, err
	}
	return usage, nil
}

// ImportTemplate imports a .docx (converted to PDF) or a directly exported
// .pdf into a new template version. Saving a file under an existing template
// name creates the next version; old versions and their generated PDFs stay
// untouched.
func (s *Service) ImportTemplate(ctx context.Context, in TemplateImportInput) (TemplateImportResult, error) {
	if strings.TrimSpace(s.dataDir) == "" {
		return TemplateImportResult{}, business("not_configured", "模板存储目录未配置")
	}
	if len(in.Data) == 0 {
		return TemplateImportResult{}, business("invalid_file", "请选择要导入的 Word 或 PDF 文件")
	}
	if len(in.Data) > maxTemplateSourceBytes {
		return TemplateImportResult{}, business("file_too_large", "模板文件过大，请压缩到 40MB 以内")
	}
	fileName := filepath.Base(strings.TrimSpace(in.FileName))
	isPDF := bytes.HasPrefix(in.Data, []byte("%PDF"))
	isZip := bytes.HasPrefix(in.Data, []byte("PK\x03\x04")) || bytes.HasPrefix(in.Data, []byte("PK\x05\x06"))
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case isPDF:
		if ext != ".pdf" && ext != "" {
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".pdf"
		}
	case isZip:
		if ext != ".docx" {
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".docx"
		}
	default:
		return TemplateImportResult{}, business("invalid_file",
			"仅支持 .docx（Word 文档）或 .pdf；如果是旧版 .doc，请先用 Word / WPS 另存为 .docx")
	}

	name := truncateRunes(strings.TrimSpace(in.Name), 120)
	name = sanitizeTemplateName(name)
	if name == "" {
		name = sanitizeTemplateName(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	}
	if name == "" {
		return TemplateImportResult{}, business("invalid_name", "模板名称不能为空")
	}

	maxVersion, err := s.rd.MaxTemplateVersion(ctx, name)
	if err != nil {
		return TemplateImportResult{}, err
	}
	version := maxVersion + 1
	id, err := model.NewID("tpl")
	if err != nil {
		return TemplateImportResult{}, err
	}
	dirRel := path.Join("templates", "pdf", id)
	dirAbs, err := s.assetFile(dirRel)
	if err != nil {
		return TemplateImportResult{}, err
	}
	if err := os.MkdirAll(dirAbs, 0o700); err != nil {
		return TemplateImportResult{}, err
	}
	cleanup := func() { _ = os.RemoveAll(dirAbs) }

	sourceExt := ".docx"
	if isPDF {
		sourceExt = ".pdf"
	}
	sourceRel := path.Join(dirRel, "source"+sourceExt)
	sourceAbs, err := s.assetFile(sourceRel)
	if err != nil {
		cleanup()
		return TemplateImportResult{}, err
	}
	if err := writeFileAtomic(sourceAbs, in.Data, 0o600); err != nil {
		cleanup()
		return TemplateImportResult{}, fmt.Errorf("保存模板源文件失败：%w", err)
	}
	sourceSHA := sha256Hex(in.Data)

	converter := "pdf"
	pdfRel := sourceRel
	pdfAbs := sourceAbs
	if !isPDF {
		convertCtx, cancel := context.WithTimeout(ctx, conversionTimeout)
		converted := filepath.Join(dirAbs, "converted.pdf")
		converter, err = s.converters.Convert(convertCtx, sourceAbs, converted)
		cancel()
		if err != nil {
			cleanup()
			if doctopdf.IsUnavailable(err) {
				return TemplateImportResult{}, business("converter_unavailable", "%s", err.Error())
			}
			return TemplateImportResult{}, business("convert_failed", "Word 转换失败：%s", err.Error())
		}
		pdfRel = path.Join(dirRel, "template.pdf")
		pdfAbs, err = s.assetFile(pdfRel)
		if err != nil {
			cleanup()
			return TemplateImportResult{}, err
		}
		if err := os.Rename(converted, pdfAbs); err != nil {
			cleanup()
			return TemplateImportResult{}, fmt.Errorf("保存转换后的 PDF 失败：%w", err)
		}
	} else {
		pdfAbs = sourceAbs
	}
	pdfRaw, err := os.ReadFile(pdfAbs)
	if err != nil {
		cleanup()
		return TemplateImportResult{}, err
	}
	pages, err := overlay.PageSizes(pdfAbs)
	if err != nil {
		cleanup()
		return TemplateImportResult{}, business("invalid_pdf", "文件不是有效的 PDF：%s", err.Error())
	}
	now := s.now()
	record := model.Template{
		ID:           id,
		Name:         name,
		Version:      version,
		Kind:         model.TemplateKindPDF,
		IsActive:     true,
		SourcePath:   filepath.ToSlash(sourceRel),
		PDFPath:      filepath.ToSlash(pdfRel),
		PageCount:    len(pages),
		Converter:    converter,
		SourceSHA256: sourceSHA,
		PDFSHA256:    sha256Hex(pdfRaw),
		Placements:   []model.Placement{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.st.PutTemplate(ctx, &record); err != nil {
		cleanup()
		return TemplateImportResult{}, err
	}
	if err := s.st.DeactivateOtherTemplateVersions(ctx, name, record.ID); err != nil {
		return TemplateImportResult{}, err
	}
	warnings := []string{}
	summary := fmt.Sprintf("导入模板「%s」v%d（%s，%d 页）", name, version, converterLabel(converter), record.PageCount)
	s.logChange(ctx, model.ChangeEntityTemplate, record.ID, model.ChangeActionImport, summary, nil)
	if version > 1 {
		warnings = append(warnings, fmt.Sprintf("已创建「%s」的新版本 v%d；旧版本继续保留，已生成 PDF 不受影响。", name, version))
	}
	if converter == "libreoffice" {
		warnings = append(warnings, "本次使用 LibreOffice 转换。个别字体或行距可能与 Word 有细微差异，请在预览中确认。")
	}
	warnings = append(warnings, "下一步：在预览页拖框标定字段、签名和公章的位置，保存后生成 PDF。")
	usage, err := s.rd.TemplateUsage(ctx, record.ID)
	if err != nil {
		return TemplateImportResult{}, err
	}
	if usage.OtherVersionContracts > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"已有 %d 份合同引用旧版本，它们仍按旧版本重出；新生成的合同默认使用 v%d。",
			usage.OtherVersionContracts, version))
	}
	return TemplateImportResult{Template: record, Usage: usage, Warnings: warnings}, nil
}

// SaveTemplateMapping saves calibrated positions as a new version of the
// template. The PDF asset is shared with the source version.
func (s *Service) SaveTemplateMapping(ctx context.Context, id string, in TemplateSaveInput) (TemplateImportResult, error) {
	current, err := s.GetTemplate(ctx, id)
	if err != nil {
		return TemplateImportResult{}, err
	}
	if current.Kind != model.TemplateKindPDF {
		return TemplateImportResult{}, business("invalid_kind", "只有 PDF 模板可以保存位置标定")
	}
	_, pages, err := s.templatePages(current)
	if err != nil {
		return TemplateImportResult{}, err
	}
	placements, err := s.validatePlacements(ctx, in.Placements, pages)
	if err != nil {
		return TemplateImportResult{}, err
	}
	overlaps := placementOverlaps(placements)
	maxVersion, err := s.rd.MaxTemplateVersion(ctx, current.Name)
	if err != nil {
		return TemplateImportResult{}, err
	}
	now := s.now()
	next := current
	next.ID = ""
	next.Version = maxVersion + 1
	next.Placements = placements
	next.IsActive = true
	next.CreatedAt = now
	next.UpdatedAt = now
	if in.IsActive != nil {
		next.IsActive = *in.IsActive
	}
	if in.MaskFieldValues != nil {
		next.MaskFieldValues = *in.MaskFieldValues
	}
	if next.ID, err = model.NewID("tpl"); err != nil {
		return TemplateImportResult{}, err
	}
	if err := s.st.PutTemplate(ctx, &next); err != nil {
		return TemplateImportResult{}, err
	}
	if next.IsActive {
		if err := s.st.DeactivateOtherTemplateVersions(ctx, next.Name, next.ID); err != nil {
			return TemplateImportResult{}, err
		}
	}
	s.logChange(ctx, model.ChangeEntityTemplate, next.ID, model.ChangeActionUpdate,
		fmt.Sprintf("保存模板「%s」v%d 的标定位置（%d 个）", next.Name, next.Version, len(placements)), nil)
	warnings := []string{}
	if len(overlaps) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"有 %d 处公章/签名框与字段框重叠超过 %.0f%%，已保存但文字可能压字，请在位置列表的重叠提醒中检查。",
			len(overlaps), overlapRatioThreshold*100))
	}
	usage, err := s.rd.TemplateUsage(ctx, next.ID)
	if err != nil {
		return TemplateImportResult{}, err
	}
	if usage.OtherVersionContracts > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"有 %d 份合同仍引用旧版本，本次修改只影响新版本 v%d；旧合同重出时仍按原版本。",
			usage.OtherVersionContracts, next.Version))
	}
	if usage.Documents > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"该模板系列已生成 %d 份 PDF，历史文件不会被改写。", usage.Documents))
	}
	return TemplateImportResult{Template: next, Usage: usage, Warnings: warnings, Overlaps: overlaps}, nil
}

// DeleteTemplateVersion removes an unreferenced template version and its
// assets. Versions referenced by contracts or generated PDFs are kept so old
// contracts can still be re-rendered.
func (s *Service) DeleteTemplateVersion(ctx context.Context, id string) error {
	tpl, err := s.GetTemplate(ctx, id)
	if err != nil {
		return err
	}
	contracts, documents, err := s.rd.TemplateReferenceCounts(ctx, tpl.ID)
	if err != nil {
		return err
	}
	if contracts > 0 || documents > 0 {
		return business("in_use",
			"模板「%s」v%d 已被 %d 份合同、%d 份 PDF 引用，不能删除；旧版本会保留以便旧合同重出",
			tpl.Name, tpl.Version, contracts, documents)
	}
	if err := s.st.DeleteTemplate(ctx, tpl.ID); err != nil {
		if isNotFound(err) {
			return business("not_found", "模板不存在")
		}
		return err
	}
	siblings, err := s.rd.ListTemplateVersions(ctx, tpl.Name)
	if err != nil {
		// 查询失败时保守处理：保留资产不删，仅记录，不影响删除本身的成功。
		slog.Warn("删除模板版本后查询同族版本失败，跳过资产清理",
			"template", tpl.ID, "name", tpl.Name, "error", err)
	} else {
		sourceUsed, pdfUsed := false, false
		for _, sibling := range siblings {
			if sibling.SourcePath == tpl.SourcePath {
				sourceUsed = true
			}
			if sibling.PDFPath == tpl.PDFPath {
				pdfUsed = true
			}
		}
		if !sourceUsed {
			s.removeAsset(tpl.SourcePath)
		}
		if !pdfUsed {
			s.removeAsset(tpl.PDFPath)
		}
	}
	s.logChange(ctx, model.ChangeEntityTemplate, tpl.ID, model.ChangeActionDelete,
		fmt.Sprintf("删除模板「%s」v%d（未生成过 PDF）", tpl.Name, tpl.Version), nil)
	return nil
}

// PreviewTemplate renders the template with one contract's data or with
// sample values. The returned preview digest must be echoed to
// GenerateContractPDF, which enforces "preview before generate".
func (s *Service) PreviewTemplate(ctx context.Context, id string, in TemplatePreviewInput) (TemplatePreviewResult, error) {
	tpl, err := s.GetTemplate(ctx, id)
	if err != nil {
		return TemplatePreviewResult{}, err
	}
	if tpl.Kind != model.TemplateKindPDF {
		return TemplatePreviewResult{}, business("invalid_kind", "只有 PDF 模板可以预览套印")
	}
	pdfAbs, pages, err := s.templatePages(tpl)
	if err != nil {
		return TemplatePreviewResult{}, err
	}
	placements, err := s.validatePlacements(ctx, tpl.Placements, pages)
	if err != nil {
		return TemplatePreviewResult{}, err
	}
	rc, err := s.loadTemplateContext(ctx, tpl, strings.TrimSpace(in.ContractID), in.Sample)
	if err != nil {
		return TemplatePreviewResult{}, err
	}
	family, err := s.FontFamily()
	if err != nil {
		return TemplatePreviewResult{}, err
	}
	built, err := overlay.Build(overlay.Options{
		Pages:           pages,
		Placements:      placements,
		Font:            family,
		Resolver:        &templateResolver{rc: rc},
		Placeholder:     true,
		MaskFieldValues: tpl.MaskFieldValues,
	})
	if err != nil {
		return TemplatePreviewResult{}, business("preview_failed", "生成预览失败：%s", err.Error())
	}
	composed, err := overlay.Compose(pdfAbs, built.PDF)
	if err != nil {
		return TemplatePreviewResult{}, business("preview_failed", "生成预览失败：%s", err.Error())
	}
	warnings := append([]overlay.Warning{}, built.Warnings...)
	warnings = append(warnings, staleSignatureWarnings(rc)...)
	return TemplatePreviewResult{
		PDF:           composed,
		Warnings:      warnings,
		PreviewSHA256: templatePreviewSHA(tpl, rc),
		PageCount:     len(pages),
		Sample:        in.Sample,
		ContractID:    rc.contract.ID,
	}, nil
}

// templatePreviewSHA is the digest a generate request must echo. It covers the
// template version, the mask toggle, the calibrated mapping and either the
// contract content or the fixed sample marker, so any change forces a fresh
// preview.
func templatePreviewSHA(tpl model.Template, rc *templateRenderContext) string {
	h := sha256.New()
	fmt.Fprintf(h, "tengshou.template.preview.v1\nid=%s\nversion=%d\nmask=%t\n",
		tpl.ID, tpl.Version, tpl.MaskFieldValues)
	if rc != nil && rc.sample {
		fmt.Fprint(h, "sample\n")
	} else if rc != nil {
		content, _ := ContentSHA256(rc.contract)
		fmt.Fprintf(h, "contract=%s\n", content)
	}
	raw, _ := json.Marshal(tpl.Placements)
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// templatePages resolves the PDF asset and its page sizes.
func (s *Service) templatePages(tpl model.Template) (string, []overlay.PageSize, error) {
	if strings.TrimSpace(tpl.PDFPath) == "" {
		return "", nil, business("invalid_template", "模板没有 PDF 文件，请重新导入")
	}
	abs, err := s.assetFile(tpl.PDFPath)
	if err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", nil, business("missing_asset", "模板 PDF 文件不存在，请重新导入模板")
	}
	pages, err := overlay.PageSizes(abs)
	if err != nil {
		return "", nil, business("invalid_pdf", "读取模板 PDF 失败：%s", err.Error())
	}
	if tpl.PageCount > 0 && tpl.PageCount != len(pages) {
		return "", nil, business("invalid_pdf", "模板 PDF 页数（%d）与记录（%d）不一致，请重新导入", len(pages), tpl.PageCount)
	}
	return abs, pages, nil
}

var contractBindableBuiltins = map[string]string{
	"contract_no": "合同编号", "year": "年度", "title": "合同名称", "type": "合同类型",
	"party_b": "相对方名称", "amount": "金额", "amount.words": "金额（大写）", "signed_on": "签订日期",
	"valid_from": "生效日期", "valid_to": "到期日期", "status": "状态",
	"lease_start": "租赁起租日", "lease_end": "租赁到期日",
	"asset_name": "房产名称", "asset_address": "房产地址",
	"asset_room_no": "房号", "asset_area": "面积（㎡）",
}

var partyBindableBuiltins = map[string]string{
	"name": "名称", "kind": "类型", "id_number": "证件号",
	"phone": "电话", "address": "地址", "note": "备注",
}

// validatePlacements normalizes and checks calibrated boxes against the real
// page sizes and the current field definitions. Errors are field-attributed
// so the editor can highlight the offending box.
func (s *Service) validatePlacements(ctx context.Context, list []model.Placement, pages []overlay.PageSize) ([]model.Placement, error) {
	if len(list) > maxPlacements {
		return nil, business("too_many_placements", "一个模板最多 %d 个标定位置", maxPlacements)
	}
	contractFields, err := s.fieldMap(ctx, model.FieldEntityContract)
	if err != nil {
		return nil, err
	}
	partyFields, err := s.fieldMap(ctx, model.FieldEntityParty)
	if err != nil {
		return nil, err
	}
	ve := &ValidationErrors{}
	seen := map[string]bool{}
	out := make([]model.Placement, 0, len(list))
	for i, raw := range list {
		p := raw
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			p.ID = fmt.Sprintf("pos_%d", i+1)
		}
		if seen[p.ID] {
			ve.add("placement."+p.ID, "标定位置 "+p.ID, "编号重复")
			continue
		}
		seen[p.ID] = true
		fieldLabel := "标定位置 " + p.ID
		if p.Page < 1 || p.Page > len(pages) {
			ve.add("placement."+p.ID, fieldLabel,
				fmt.Sprintf("页码 %d 超出模板页数 1–%d", p.Page, len(pages)))
			continue
		}
		page := pages[p.Page-1]
		if !finite(p.X) || !finite(p.Y) || !finite(p.W) || !finite(p.H) {
			ve.add("placement."+p.ID, fieldLabel, "坐标必须是有效数字")
			continue
		}
		if p.W < 2 || p.H < 2 {
			ve.add("placement."+p.ID, fieldLabel, "位置框太小（宽高至少 2pt）")
			continue
		}
		if p.X < -0.5 || p.Y < -0.5 ||
			p.X+p.W > page.Width+1 || p.Y+p.H > page.Height+1 {
			ve.add("placement."+p.ID, fieldLabel,
				fmt.Sprintf("超出第 %d 页范围（页面 %.0f×%.0f pt）", p.Page, page.Width, page.Height))
			continue
		}
		p.X = clampPt(p.X, 0, page.Width-2)
		p.Y = clampPt(p.Y, 0, page.Height-2)
		if p.X+p.W > page.Width {
			p.W = page.Width - p.X
		}
		if p.Y+p.H > page.Height {
			p.H = page.Height - p.Y
		}
		p.X = overlay.RoundToPt(p.X)
		p.Y = overlay.RoundToPt(p.Y)
		p.W = overlay.RoundToPt(p.W)
		p.H = overlay.RoundToPt(p.H)
		if p.FontSize != 0 && (p.FontSize < 4 || p.FontSize > 72) {
			ve.add("placement."+p.ID, fieldLabel, "字号应在 4–72pt 之间")
			continue
		}
		switch p.Align = strings.ToLower(strings.TrimSpace(p.Align)); p.Align {
		case "":
			p.Align = "left"
		case "left", "center", "right":
		default:
			ve.add("placement."+p.ID, fieldLabel, "对齐方式只能是 left / center / right")
			continue
		}
		switch p.Type {
		case model.PlacementField:
			p.Party, p.SealMode, p.SealID = "", "", ""
			if err := validateBinding(p.Field, contractFields, partyFields); err != nil {
				ve.add("placement."+p.ID, fieldLabel, err.Error())
				continue
			}
		case model.PlacementSignature:
			p.Field, p.SealMode, p.SealID = "", "", ""
			p.FontSize, p.Align = 0, ""
			if p.Party != model.PlacementPartyA && p.Party != model.PlacementPartyB {
				ve.add("placement."+p.ID, fieldLabel, "签名必须绑定甲方或乙方")
				continue
			}
		case model.PlacementSeal:
			p.Field, p.FontSize, p.Align = "", 0, ""
			p.SealMode = strings.ToLower(strings.TrimSpace(p.SealMode))
			switch p.SealMode {
			case "":
				p.SealMode = model.PlacementSealContract
			case model.PlacementSealContract:
				p.SealID = ""
			case model.PlacementSealFixed:
				if strings.TrimSpace(p.SealID) == "" {
					ve.add("placement."+p.ID, fieldLabel, "请选择要落位的公章图片")
					continue
				}
				if _, err := s.rd.GetSeal(ctx, strings.TrimSpace(p.SealID)); err != nil {
					ve.add("placement."+p.ID, fieldLabel, "选择的公章图片不存在")
					continue
				}
			default:
				ve.add("placement."+p.ID, fieldLabel, "公章绑定方式只能是 contract 或 seal")
				continue
			}
			if p.Party != "" && p.Party != model.PlacementPartyA && p.Party != model.PlacementPartyB {
				ve.add("placement."+p.ID, fieldLabel, "公章所属方只能是甲方或乙方")
				continue
			}
		default:
			ve.add("placement."+p.ID, fieldLabel, "绑定类型只能是 field / signature / seal")
			continue
		}
		out = append(out, p)
	}
	if err := ve.errOrNil(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) fieldMap(ctx context.Context, entity string) (map[string]model.FieldDefinition, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, entity, true)
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.FieldDefinition, len(fields))
	for _, field := range fields {
		out[field.Key] = field
	}
	return out, nil
}

// validateBinding checks one placement field key against the field catalogue.
func validateBinding(key string, contractFields, partyFields map[string]model.FieldDefinition) error {
	key = strings.TrimSpace(key)
	prefix, rest, ok := strings.Cut(key, ".")
	if !ok || rest == "" {
		return fmt.Errorf("字段绑定格式应为 contract.<键> / party_a.<键> / party_b.<键>")
	}
	var builtins map[string]string
	var fields map[string]model.FieldDefinition
	var label string
	switch prefix {
	case "contract":
		builtins, fields, label = contractBindableBuiltins, contractFields, "合同"
	case "party_a", "party_b":
		builtins, fields, label = partyBindableBuiltins, partyFields, "相对方"
	default:
		return fmt.Errorf("字段绑定必须以 contract. / party_a. / party_b. 开头")
	}
	if dataKey, ok := strings.CutPrefix(rest, "data."); ok {
		if dataKey == "" {
			return fmt.Errorf("缺少字段键")
		}
		words := false
		if key, ok := strings.CutSuffix(dataKey, ".words"); ok {
			dataKey, words = key, true
			if dataKey == "" {
				return fmt.Errorf("缺少字段键")
			}
		}
		field, exists := fields[dataKey]
		if !exists {
			return fmt.Errorf("字段「%s」不在%s字段中，请先在「字段」页新增", dataKey, label)
		}
		if field.Type == model.FieldTypeImage {
			return fmt.Errorf("图片字段「%s」不能作为文字绑定，请改用签名或公章位置", field.Label)
		}
		if words && (prefix != "contract" || field.Type != model.FieldTypeMoney) {
			return fmt.Errorf("只有合同的金额字段可以绑定大写")
		}
		if field.BuiltIn {
			hintKey := dataKey
			if words {
				hintKey += ".words"
			}
			return fmt.Errorf("内置字段请直接使用 %s.%s，不要加 data. 前缀", prefix, hintKey)
		}
		return nil
	}
	if _, exists := builtins[rest]; !exists {
		return fmt.Errorf("未知的%s字段「%s」", label, rest)
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func clampPt(v, min, max float64) float64 {
	if max < min {
		max = min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// templateRenderContext bundles the data used to fill one template preview or
// generated PDF.
type templateRenderContext struct {
	svc            *Service
	ctx            context.Context
	template       model.Template
	contract       model.Contract
	partyA         *model.Party
	partyB         *model.Party
	asset          *model.Asset
	signatures     []model.Signature
	seals          []model.Seal
	attachedSeals  []model.Seal
	contractFields map[string]model.FieldDefinition
	partyFields    map[string]model.FieldDefinition
	sample         bool
}

func (s *Service) loadTemplateContext(ctx context.Context, tpl model.Template, contractID string, sample bool) (*templateRenderContext, error) {
	contractFields, err := s.fieldMap(ctx, model.FieldEntityContract)
	if err != nil {
		return nil, err
	}
	partyFields, err := s.fieldMap(ctx, model.FieldEntityParty)
	if err != nil {
		return nil, err
	}
	rc := &templateRenderContext{
		svc: s, ctx: ctx, template: tpl, contractFields: contractFields, partyFields: partyFields, sample: sample,
	}
	if sample {
		contract, err := s.sampleContract(ctx)
		if err != nil {
			return nil, err
		}
		rc.contract = contract
		rc.partyA, rc.partyB = s.sampleParties(ctx)
		rc.asset = sampleAsset()
		sig, ok, err := s.sampleSignature(ctx)
		if err != nil {
			return nil, err
		}
		if ok {
			rc.signatures = []model.Signature{sig}
		}
		seal, err := s.sampleSeal(ctx)
		if err != nil {
			return nil, err
		}
		if seal != nil {
			rc.seals = []model.Seal{*seal}
			rc.attachedSeals = rc.seals
		}
		return rc, nil
	}
	detail, err := s.rd.GetContractDetail(ctx, strings.TrimSpace(contractID))
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "合同不存在")
		}
		return nil, err
	}
	rc.contract = detail.Contract
	rc.partyA, rc.partyB = detail.PartyA, detail.PartyB
	rc.asset = detail.Asset
	rc.signatures = detail.Signatures
	allSeals, attached, err := s.SealsForContract(ctx, detail.Contract)
	if err != nil {
		return nil, err
	}
	rc.seals, rc.attachedSeals = allSeals, attached
	return rc, nil
}

// templateResolver adapts a render context to overlay.Resolver.
type templateResolver struct {
	rc *templateRenderContext
}

func (r *templateResolver) Text(p model.Placement) (string, bool) {
	value := r.rc.textValue(p.Field)
	return value, strings.TrimSpace(value) != ""
}

func (r *templateResolver) Image(p model.Placement) (image.Image, bool, error) {
	switch p.Type {
	case model.PlacementSignature:
		return r.rc.signatureImage(p)
	case model.PlacementSeal:
		return r.rc.sealImage(p)
	}
	return nil, false, nil
}

func (rc *templateRenderContext) textValue(key string) string {
	key = strings.TrimSpace(key)
	prefix, rest, ok := strings.Cut(key, ".")
	if !ok {
		return ""
	}
	switch prefix {
	case "contract":
		return rc.contractValue(rest)
	case "party_a":
		return rc.partyValue(rc.partyA, rest)
	case "party_b":
		return rc.partyValue(rc.partyB, rest)
	}
	return ""
}

func (rc *templateRenderContext) contractValue(rest string) string {
	switch rest {
	case "contract_no":
		return rc.contract.ContractNo
	case "year":
		if rc.contract.Year == 0 {
			return ""
		}
		return strconv.Itoa(rc.contract.Year)
	case "title":
		return rc.contract.Title
	case "type":
		return rc.contract.Type
	case "party_b":
		if rc.partyB != nil {
			return rc.partyB.Name
		}
		return ""
	case "amount":
		if rc.contract.Amount == 0 {
			return ""
		}
		return formatMoneyValue(rc.contract.Amount)
	case "amount.words":
		// Manual override wins; otherwise render the built-in amount in words.
		if override := wordsOverride(rc.contract.Data, model.FieldAmount); override != "" {
			return override
		}
		return MoneyWords(rc.contract.Amount)
	case "signed_on":
		return chineseDate(rc.contract.SignedOn)
	case "valid_from":
		return chineseDate(rc.contract.ValidFrom)
	case "valid_to":
		return chineseDate(rc.contract.ValidTo)
	case "lease_start":
		return chineseDate(rc.contract.LeaseStart)
	case "lease_end":
		return chineseDate(rc.contract.LeaseEnd)
	case "asset_name":
		if rc.asset != nil {
			return rc.asset.Name
		}
		return ""
	case "asset_address":
		if rc.asset != nil {
			return rc.asset.Address
		}
		return ""
	case "asset_room_no":
		if rc.asset != nil {
			return rc.asset.RoomNo
		}
		return ""
	case "asset_area":
		if rc.asset != nil && rc.asset.AreaSqm != nil {
			return strconv.FormatFloat(*rc.asset.AreaSqm, 'f', -1, 64)
		}
		return ""
	case "status":
		return StatusLabel(rc.contract.Status)
	}
	if dataKey, ok := strings.CutPrefix(rest, "data."); ok {
		if fieldKey, ok := strings.CutSuffix(dataKey, ".words"); ok {
			field, exists := rc.contractFields[fieldKey]
			if !exists || field.Type != model.FieldTypeMoney {
				return ""
			}
			if override := wordsOverride(rc.contract.Data, fieldKey); override != "" {
				return override
			}
			if value, ok := parseNumber(rawString(rc.contract.Data[fieldKey])); ok {
				return MoneyWords(value)
			}
			return ""
		}
		field, exists := rc.contractFields[dataKey]
		if !exists {
			return ""
		}
		return formatDynamicValue(field, rc.contract.Data[dataKey])
	}
	return ""
}

func (rc *templateRenderContext) partyValue(party *model.Party, rest string) string {
	if party == nil {
		return ""
	}
	switch rest {
	case "name":
		return party.Name
	case "kind":
		return KindLabel(party.Kind)
	case "id_number":
		return party.IDNumber
	case "phone":
		return party.Phone
	case "address":
		return party.Address
	case "note":
		return party.Note
	}
	if dataKey, ok := strings.CutPrefix(rest, "data."); ok {
		field, exists := rc.partyFields[dataKey]
		if !exists {
			return ""
		}
		return formatDynamicValue(field, party.Data[dataKey])
	}
	return ""
}

func (rc *templateRenderContext) signatureImage(p model.Placement) (image.Image, bool, error) {
	sig, ok := rc.signatureFor(p)
	if !ok {
		return nil, false, nil
	}
	img, err := rc.svc.loadImageAsset(sig.AssetPath)
	if err != nil {
		return nil, false, err
	}
	return img, true, nil
}

// signatureFor picks the newest signature for the placement party. There is
// no cross-party fallback: a missing signature must be visible as such. A
// sample preview has no party IDs to match, so it uses the newest signature.
func (rc *templateRenderContext) signatureFor(p model.Placement) (model.Signature, bool) {
	if rc.sample {
		if len(rc.signatures) > 0 {
			return rc.signatures[0], true
		}
		return model.Signature{}, false
	}
	partyID := ""
	switch p.Party {
	case model.PlacementPartyA:
		if rc.partyA != nil {
			partyID = rc.partyA.ID
		}
	case model.PlacementPartyB:
		if rc.partyB != nil {
			partyID = rc.partyB.ID
		}
	}
	for _, sig := range rc.signatures {
		if partyID == "" || sig.PartyID == partyID {
			return sig, true
		}
	}
	return model.Signature{}, false
}

func (rc *templateRenderContext) sealImage(p model.Placement) (image.Image, bool, error) {
	seal, ok, err := rc.sealFor(p)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	img, err := rc.svc.loadImageAsset(seal.AssetPath)
	if err != nil {
		return nil, false, err
	}
	return img, true, nil
}

func (rc *templateRenderContext) sealFor(p model.Placement) (model.Seal, bool, error) {
	if p.SealMode == model.PlacementSealFixed && strings.TrimSpace(p.SealID) != "" {
		for _, seal := range rc.seals {
			if seal.ID == p.SealID {
				return seal, true, nil
			}
		}
		// A pinned seal does not need to be linked to the contract; look it
		// up directly so the calibration is honoured for every contract.
		seal, err := rc.svc.rd.GetSeal(rc.ctx, strings.TrimSpace(p.SealID))
		if err != nil {
			if isNotFound(err) {
				return model.Seal{}, false, fmt.Errorf("选择的公章图片不存在")
			}
			return model.Seal{}, false, err
		}
		return seal, true, nil
	}
	partyID := ""
	switch p.Party {
	case model.PlacementPartyA:
		if rc.partyA != nil {
			partyID = rc.partyA.ID
		}
	case model.PlacementPartyB:
		if rc.partyB != nil {
			partyID = rc.partyB.ID
		}
	}
	if partyID != "" {
		for _, seal := range rc.seals {
			if seal.PartyID == partyID {
				return seal, true, nil
			}
		}
	}
	if len(rc.attachedSeals) > 0 {
		return rc.attachedSeals[0], true, nil
	}
	if partyID == "" && len(rc.seals) > 0 {
		return rc.seals[0], true, nil
	}
	return model.Seal{}, false, nil
}

// loadImageAsset decodes a PNG/JPEG asset relative to the data directory.
func (s *Service) loadImageAsset(rel string) (image.Image, error) {
	abs, err := s.assetFile(rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("图片文件不存在：%s", rel)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("图片 %s 无法解码：%w", rel, err)
	}
	return img, nil
}

// staleSignatureWarnings flags placements whose on-site signature was
// collected for an older contract content version. Archive reuse (v0.2 D3=B)
// is never stale: it deliberately carries no content version.
func staleSignatureWarnings(rc *templateRenderContext) []overlay.Warning {
	if rc == nil || rc.sample {
		return nil
	}
	content, err := ContentSHA256(rc.contract)
	if err != nil || content == "" {
		return nil
	}
	out := []overlay.Warning{}
	for _, p := range rc.template.Placements {
		if p.Type != model.PlacementSignature {
			continue
		}
		sig, ok := rc.signatureFor(p)
		if !ok || sig.Source == model.SignatureSourceArchive ||
			sig.ContentSHA256 == "" || sig.ContentSHA256 == content {
			continue
		}
		out = append(out, overlay.Warning{
			Code:        "stale_signature",
			PlacementID: p.ID,
			Message: fmt.Sprintf("位置 %s 的签名采集于合同内容修改前（%s…），请核对签名是否仍然有效",
				p.ID, shortSHA(sig.ContentSHA256)),
		})
	}
	return out
}

// formatMoneyValue renders a number with two decimals and thousands commas.
func formatMoneyValue(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	var grouped strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}
	out := grouped.String()
	if len(parts) == 2 {
		out += "." + parts[1]
	}
	if negative {
		out = "-" + out
	}
	return out
}

var chineseDateLayouts = []string{"2006-01-02", "2006/01/02", time.RFC3339}

func chineseDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range chineseDateLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day())
		}
	}
	return raw
}

func formatDynamicValue(field model.FieldDefinition, raw any) string {
	if raw == nil {
		return ""
	}
	switch field.Type {
	case model.FieldTypeDate:
		return chineseDate(displayValue(raw))
	case model.FieldTypeMoney:
		if v, ok := parseNumber(rawString(raw)); ok {
			return formatMoneyValue(v)
		}
		return displayValue(raw)
	case model.FieldTypeBool:
		if v, ok := parseBoolValue(rawString(raw)); ok {
			if v {
				return "是"
			}
			return "否"
		}
		return displayValue(raw)
	default:
		return displayValue(raw)
	}
}

func sampleFieldValue(field model.FieldDefinition) (any, bool) {
	if field.Type == model.FieldTypeImage {
		return nil, false
	}
	if strings.TrimSpace(field.DefaultValue) != "" {
		switch field.Type {
		case model.FieldTypeBool:
			if v, ok := parseBoolValue(field.DefaultValue); ok {
				return v, true
			}
		case model.FieldTypeNumber, model.FieldTypeMoney:
			if v, ok := parseNumber(field.DefaultValue); ok {
				return v, true
			}
		default:
			return field.DefaultValue, true
		}
	}
	switch field.Type {
	case model.FieldTypeText, model.FieldTypeLongText:
		return "示例" + field.Label, true
	case model.FieldTypeNumber:
		return float64(100), true
	case model.FieldTypeMoney:
		return float64(1000), true
	case model.FieldTypeDate:
		return "2026-01-01", true
	case model.FieldTypeSelect:
		if len(field.Options) > 0 {
			return field.Options[0], true
		}
		return "示例选项", true
	case model.FieldTypeBool:
		return true, true
	}
	return nil, false
}

func (s *Service) sampleContract(ctx context.Context) (model.Contract, error) {
	data := map[string]any{}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return model.Contract{}, err
	}
	for _, field := range fields {
		if field.BuiltIn || field.Archived {
			continue
		}
		if value, ok := sampleFieldValue(field); ok {
			data[field.Key] = value
		}
	}
	return model.Contract{
		ID:         "sample-contract",
		ContractNo: "HT-2026-0001",
		Year:       2026,
		Title:      "示例合同（预览）",
		Type:       "房屋租赁",
		Status:     model.ContractActive,
		SignedOn:   "2026-01-01",
		ValidFrom:  "2026-02-01",
		ValidTo:    "2027-01-31",
		AssetID:    "sample-asset",
		LeaseStart: "2026-02-01",
		LeaseEnd:   "2027-01-31",
		Amount:     12000,
		Data:       data,
	}, nil
}

// sampleAsset supplies placeholder values for asset bindings in previews.
func sampleAsset() *model.Asset {
	area := 88.5
	return &model.Asset{
		ID: "sample-asset", AssetType: model.AssetTypeHouse,
		Name: "示例房产", Address: "示例路 3 号", RoomNo: "2 单元 501",
		AreaSqm: &area, Enabled: true, LeaseStatus: model.LeaseStatusVacant,
	}
}

func (s *Service) sampleParties(ctx context.Context) (*model.Party, *model.Party) {
	partyA := &model.Party{
		ID: "sample-party-a", Kind: model.PartyOrganization, Name: "示例甲方单位",
		IDNumber: "91110000MA00000000", Phone: "13800000001", Address: "示例地址 1 号",
		Data: map[string]any{},
	}
	partyB := &model.Party{
		ID: "sample-party-b", Kind: model.PartyPerson, Name: "示例乙方",
		IDNumber: "11010119900101001X", Phone: "13800000002", Address: "示例地址 2 号",
		Data: map[string]any{},
	}
	if fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityParty, false); err == nil {
		for _, field := range fields {
			if field.BuiltIn || field.Archived {
				continue
			}
			if value, ok := sampleFieldValue(field); ok {
				partyA.Data[field.Key] = value
				partyB.Data[field.Key] = value
			}
		}
	}
	return partyA, partyB
}

// sampleSignature returns the newest retained signature of any party, so a
// sample preview shows how signatures land on the page.
func (s *Service) sampleSignature(ctx context.Context) (model.Signature, bool, error) {
	list, err := s.rd.ListSignatures(ctx, model.ListFilter{Limit: 1})
	if err != nil {
		return model.Signature{}, false, err
	}
	if len(list.Items) == 0 {
		return model.Signature{}, false, nil
	}
	return list.Items[0], true, nil
}

func (s *Service) sampleSeal(ctx context.Context) (*model.Seal, error) {
	list, err := s.rd.ListSeals(ctx, model.ListFilter{Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	items := list.Items
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return &items[0], nil
}

func converterLabel(name string) string {
	switch name {
	case "word":
		return "Microsoft Word"
	case "libreoffice":
		return "LibreOffice"
	case "pdf":
		return "PDF 直接导入"
	}
	return name
}

func sanitizeTemplateName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
