package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// AssetMigrationSkipped is one contract the migration left untouched, with the
// reason it could not be migrated.
type AssetMigrationSkipped struct {
	ContractID string `json:"contract_id"`
	ContractNo string `json:"contract_no"`
	Title      string `json:"title"`
	Reason     string `json:"reason"`
}

// AssetMigrationAsset is one property archive created by the migration. ID is
// empty during a dry run because nothing has been written yet.
type AssetMigrationAsset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	// Source names the custom field the address was read from.
	Source string `json:"source"`
}

// AssetMigrationReport summarizes one legacy address migration run.
type AssetMigrationReport struct {
	DryRun bool `json:"dry_run"`
	// BackupPath is the full backup package created before writing; empty for
	// dry runs.
	BackupPath       string `json:"backup_path,omitempty"`
	ScannedContracts int    `json:"scanned_contracts"`
	// CreatedAssets lists the archives this run created (or would create in a
	// dry run).
	CreatedAssets []AssetMigrationAsset `json:"created_assets"`
	// LinkedContracts counts contracts that received an asset_id.
	LinkedContracts int `json:"linked_contracts"`
	// MergedContracts counts linked contracts whose address matched an asset
	// that already existed or was created earlier in the same run.
	MergedContracts int `json:"merged_contracts"`
	// AlreadyLinked counts contracts that already had an asset link and were
	// left alone (this is what makes re-running the migration a no-op).
	AlreadyLinked int                     `json:"already_linked"`
	Skipped       []AssetMigrationSkipped `json:"skipped"`
}

// Address detection heuristics. They are deliberately conservative: only
// user-defined text fields are scanned, property-address keywords must not
// look like contact/mailing addresses, and a contract with several conflicting
// candidates is skipped rather than guessed.
var (
	// Strong keywords: the field is clearly about where the property is.
	migrationAddressStrongKeywords = []string{"地址", "坐落", "门牌", "租赁物", "标的物"}
	// Weak keywords only count when the value also looks like an address, so a
	// "房号" field holding "302" is never treated as a full address.
	migrationAddressWeakKeywords = []string{
		"房屋", "房产", "房号", "房间", "住房", "租住", "宿舍", "门面", "铺面", "物业",
	}
	// Negative keywords veto the field entirely: these are addresses of people
	// or organisations, not of the rented property.
	migrationAddressNegativeKeywords = []string{
		"通讯", "邮寄", "联系", "户籍", "身份证", "注册地址", "发票", "快递", "收件",
	}
	migrationAddressAreaTokens     = []string{"省", "市", "区", "县", "镇", "街道", "乡", "村", "路", "街", "巷", "小区", "社区", "园区"}
	migrationAddressBuildingTokens = []string{"号", "栋", "幢", "单元", "室", "楼", "层", "户", "座"}
	// Clause words and sentence punctuation mean the value is a contract
	// sentence quoting an address, not an address stored as its own value.
	migrationAddressClauseTokens = []string{"甲方", "乙方", "出租", "承租", "租期", "合同", "协议", "条款"}
	migrationAddressNoise        = map[string]bool{
		"无": true, "暂无": true, "没有": true, "无地址": true, "不适用": true,
		"见附件": true, "详见合同": true, "详见附件": true, "同甲方": true,
		"同乙方": true, "-": true, "--": true, "/": true, "0": true,
		"n/a": true, "na": true,
	}
)

type migrationFieldKind int

const (
	migrationFieldOther migrationFieldKind = iota
	migrationFieldWeak
	migrationFieldStrong
	migrationFieldNegative
)

// migrationClassifyField maps a custom field's label/key to a detection class.
func migrationClassifyField(label, key string) migrationFieldKind {
	text := strings.ToLower(strings.TrimSpace(label) + " " + strings.TrimSpace(key))
	for _, keyword := range migrationAddressNegativeKeywords {
		if strings.Contains(text, keyword) {
			return migrationFieldNegative
		}
	}
	for _, keyword := range migrationAddressStrongKeywords {
		if strings.Contains(text, keyword) {
			return migrationFieldStrong
		}
	}
	for _, keyword := range migrationAddressWeakKeywords {
		if strings.Contains(text, keyword) {
			return migrationFieldWeak
		}
	}
	return migrationFieldOther
}

// migrationAddressUsable rejects blanks and placeholder values that mean "no
// address" rather than an address.
func migrationAddressUsable(value string) bool {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) < 2 {
		return false
	}
	return !migrationAddressNoise[strings.ToLower(value)]
}

// migrationLooksLikeAddress is the content-shape fallback: an administrative
// token plus a building/unit token, short enough to be an address value rather
// than a contract sentence quoting one.
func migrationLooksLikeAddress(value string) bool {
	n := utf8.RuneCountInString(value)
	if n < 6 || n > 80 {
		return false
	}
	if strings.ContainsAny(value, "，。；：、！？,;:!?") {
		return false
	}
	if migrationContainsAny(value, migrationAddressClauseTokens) {
		return false
	}
	return migrationContainsAny(value, migrationAddressAreaTokens) &&
		migrationContainsAny(value, migrationAddressBuildingTokens)
}

func migrationContainsAny(value string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

// migrationAddressKey normalizes an address for duplicate detection. All
// whitespace is removed so "幸福路 1 号" and "幸福路1号" merge.
func migrationAddressKey(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

type migrationCandidate struct {
	label string
	value string
	// dedicated is true when the field's name marks it as an address field.
	dedicated bool
}

// migrationCandidates inspects one contract's custom text fields and returns
// the distinct address candidates. rejected carries human-readable notes about
// address-like fields whose values were not usable, so the report can explain
// why a contract was skipped. A dedicated address field always wins over
// incidental address-shaped text (for example a clause quoting the address).
func migrationCandidates(contract model.Contract, fields []model.FieldDefinition) (candidates []migrationCandidate, rejected []string) {
	index := map[string]int{}
	for _, field := range fields {
		if field.BuiltIn {
			continue
		}
		if field.Type != model.FieldTypeText && field.Type != model.FieldTypeLongText {
			continue
		}
		value := strings.TrimSpace(displayValue(contract.Data[field.Key]))
		if value == "" {
			continue
		}
		kind := migrationClassifyField(field.Label, field.Key)
		if kind == migrationFieldNegative {
			continue
		}
		usable := migrationAddressUsable(value)
		shape := migrationLooksLikeAddress(value)
		take := false
		switch kind {
		case migrationFieldStrong:
			take = usable
		case migrationFieldWeak, migrationFieldOther:
			take = usable && shape
		}
		if !take {
			if kind == migrationFieldStrong || kind == migrationFieldWeak {
				rejected = append(rejected, fmt.Sprintf("字段「%s」值「%s」不像完整地址",
					field.Label, truncateRunes(value, 40)))
			}
			continue
		}
		key := migrationAddressKey(value)
		if key == "" {
			continue
		}
		dedicated := kind == migrationFieldStrong || kind == migrationFieldWeak
		if at, exists := index[key]; exists {
			candidates[at].dedicated = candidates[at].dedicated || dedicated
			continue
		}
		index[key] = len(candidates)
		candidates = append(candidates, migrationCandidate{label: field.Label, value: value, dedicated: dedicated})
	}
	hasDedicated := false
	for _, candidate := range candidates {
		if candidate.dedicated {
			hasDedicated = true
			break
		}
	}
	if hasDedicated {
		dedicatedOnly := make([]migrationCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.dedicated {
				dedicatedOnly = append(dedicatedOnly, candidate)
			}
		}
		candidates = dedicatedOnly
	}
	return candidates, rejected
}

// migrationPlanItem is one contract scheduled to receive an asset link.
type migrationPlanItem struct {
	contract model.Contract
	address  string
	key      string
	// merged is true when the address matched an archive that already existed
	// or was claimed by an earlier contract in the same run.
	merged bool
}

// MigrateAssets turns address values stored in contract custom fields into
// property archives and links the contracts with asset_id (te-ft6te). It is
// idempotent: contracts that already have an asset link are counted and left
// alone, and duplicate addresses reuse an existing archive. A full backup
// package is created before anything is written; dryRun reports the plan and
// writes nothing at all. Original custom-field values are never modified.
func (s *Service) MigrateAssets(ctx context.Context, dryRun bool) (AssetMigrationReport, error) {
	report := AssetMigrationReport{
		DryRun:        dryRun,
		CreatedAssets: []AssetMigrationAsset{},
		Skipped:       []AssetMigrationSkipped{},
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, true)
	if err != nil {
		return report, fmt.Errorf("读取合同字段定义: %w", err)
	}
	contracts, err := pageAll(func(offset int) (model.List[model.Contract], error) {
		return s.rd.ListContracts(ctx, model.ListFilter{Limit: 500, Offset: offset})
	})
	if err != nil {
		return report, fmt.Errorf("读取合同: %w", err)
	}
	assets, err := pageAll(func(offset int) (model.List[model.Asset], error) {
		return s.rd.ListAssets(ctx, model.ListFilter{Limit: 500, Offset: offset})
	})
	if err != nil {
		return report, fmt.Errorf("读取房产档案: %w", err)
	}
	// byAddress maps a normalized address to the archive that represents it.
	// Planned archives are reserved here with an empty ID so a second contract
	// with the same address is planned as a merge, not a second archive.
	byAddress := map[string]model.Asset{}
	for _, asset := range assets {
		key := migrationAddressKey(asset.Address)
		if key == "" {
			continue
		}
		if _, exists := byAddress[key]; !exists {
			byAddress[key] = asset
		}
	}
	plan := []migrationPlanItem{}
	plannedOrder := []string{}
	report.ScannedContracts = len(contracts)
	for _, contract := range contracts {
		if strings.TrimSpace(contract.AssetID) != "" {
			report.AlreadyLinked++
			continue
		}
		candidates, rejected := migrationCandidates(contract, fields)
		if len(candidates) == 0 {
			reason := "未找到地址类自定义字段或字段为空"
			if len(rejected) > 0 {
				reason = "地址类字段无法识别为完整地址：" + strings.Join(rejected, "；")
			}
			report.Skipped = append(report.Skipped, AssetMigrationSkipped{
				ContractID: contract.ID, ContractNo: contract.ContractNo,
				Title: contract.Title, Reason: reason,
			})
			continue
		}
		if len(candidates) > 1 {
			parts := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				parts = append(parts, fmt.Sprintf("字段「%s」为「%s」",
					candidate.label, truncateRunes(candidate.value, 40)))
			}
			report.Skipped = append(report.Skipped, AssetMigrationSkipped{
				ContractID: contract.ID, ContractNo: contract.ContractNo,
				Title:  contract.Title,
				Reason: "多个候选地址取值不一致，无法确定：" + strings.Join(parts, "；"),
			})
			continue
		}
		candidate := candidates[0]
		address := strings.Join(strings.Fields(candidate.value), " ")
		key := migrationAddressKey(candidate.value)
		asset, found := byAddress[key]
		if !found {
			asset = model.Asset{Address: address}
			byAddress[key] = asset
			plannedOrder = append(plannedOrder, key)
			report.CreatedAssets = append(report.CreatedAssets, AssetMigrationAsset{
				Name: truncateRunes(address, 120), Address: address, Source: candidate.label,
			})
		}
		plan = append(plan, migrationPlanItem{
			contract: contract, address: address, key: key, merged: found,
		})
	}
	report.LinkedContracts = len(plan)
	for _, item := range plan {
		if item.merged {
			report.MergedContracts++
		}
	}

	// Nothing to do: skip the backup entirely so a repeated run is a no-op.
	if dryRun || len(plan) == 0 {
		return report, nil
	}

	backupPath, manifest, err := s.CreateBackupFile(ctx)
	if err != nil {
		return report, fmt.Errorf("迁移前自动备份失败: %w", err)
	}
	report.BackupPath = backupPath
	auditID := manifest.ID
	if strings.TrimSpace(auditID) == "" {
		auditID = "assets"
	}

	// Create the planned archives first, in the order their first contract
	// appeared, so the report and the archive list stay deterministic.
	for index, key := range plannedOrder {
		planned := report.CreatedAssets[index]
		enabled := true
		asset, _, err := s.SaveAsset(ctx, AssetInput{
			AssetType: model.AssetTypeHouse,
			Name:      truncateRunes(planned.Address, 120),
			Address:   planned.Address,
			Note:      fmt.Sprintf("存量迁移自动创建（来源字段「%s」）", planned.Source),
			Enabled:   &enabled,
		})
		if err != nil {
			return report, fmt.Errorf("创建房产档案「%s」: %w", planned.Address, err)
		}
		byAddress[key] = asset
		report.CreatedAssets[index].ID = asset.ID
		report.CreatedAssets[index].Name = asset.Name
	}
	for _, item := range plan {
		assetID := byAddress[item.key].ID
		if assetID == "" {
			return report, fmt.Errorf("迁移计划缺少房产档案：%s", item.address)
		}
		contract := item.contract
		contract.AssetID = assetID
		if err := s.st.PutContract(ctx, &contract); err != nil {
			return report, fmt.Errorf("关联合同 %s 与房产档案: %w", contract.ContractNo, err)
		}
	}

	s.logChange(ctx, model.ChangeEntityMigration, auditID, model.ChangeActionImport,
		fmt.Sprintf("存量资产迁移：扫描 %d 份合同，新建房产档案 %d 个，关联合同 %d 份"+
			"（其中合并到已有档案 %d 份），跳过 %d 份",
			report.ScannedContracts, len(report.CreatedAssets), report.LinkedContracts,
			report.MergedContracts, len(report.Skipped)), nil)
	return report, nil
}
