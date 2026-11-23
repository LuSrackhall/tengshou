package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// ContractInput is the editable payload for a contract. Core fields are typed
// because they are first-class columns; Data carries dynamic field values by
// field key. Source is only used for audit summaries ("excel" during import).
type ContractInput struct {
	ID         string  `json:"id"`
	ContractNo string  `json:"contract_no"`
	Year       int     `json:"year"`
	Title      string  `json:"title"`
	Type       string  `json:"type"`
	PartyAID   string  `json:"party_a_id"`
	PartyBID   string  `json:"party_b_id"`
	PartyB     string  `json:"party_b"`
	Status     string  `json:"status"`
	SignedOn   string  `json:"signed_on"`
	ValidFrom  string  `json:"valid_from"`
	ValidTo    string  `json:"valid_to"`
	Amount     float64 `json:"amount"`
	TemplateID string  `json:"template_id"`
	// SourceContractID records the contract a new contract is renewed from
	// (B7 provenance). It is only honored when creating; updates ignore it.
	SourceContractID string         `json:"source_contract_id,omitempty"`
	Data             map[string]any `json:"data"`
	Source           string         `json:"-"`
	CreateParty      *bool          `json:"create_party,omitempty"`
}

// SaveContract creates or updates a contract. Contract numbers are allocated
// inside a write lock and are immutable after creation.
func (s *Service) SaveContract(ctx context.Context, in ContractInput) (model.Contract, []string, error) {
	unlock := s.writeLock()
	defer unlock()

	var existing *model.Contract
	if id := strings.TrimSpace(in.ID); id != "" {
		c, err := s.rd.GetContract(ctx, id)
		if err != nil {
			if isNotFound(err) {
				return model.Contract{}, nil, business("not_found", "合同不存在")
			}
			return model.Contract{}, nil, err
		}
		existing = &c
	}

	var contract model.Contract
	var sourceNo string
	var warnings []string
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		contract, sourceNo, warnings, err = s.buildContract(ctx, in, existing, false)
		if err != nil {
			return model.Contract{}, nil, err
		}
		err = s.st.PutContract(ctx, &contract)
		if err == nil {
			break
		}
		if errors.Is(err, store.ErrDuplicateContractNo) && existing == nil && strings.TrimSpace(in.ContractNo) == "" {
			continue
		}
		return model.Contract{}, nil, err
	}
	if err != nil {
		return model.Contract{}, nil, err
	}

	action := model.ChangeActionCreate
	prefix := ""
	if in.Source == "excel" {
		prefix = "Excel 导入："
	}
	summary := fmt.Sprintf("%s新建合同 %s「%s」", prefix, contract.ContractNo, contract.Title)
	var changes []model.FieldChange
	if existing != nil {
		action = model.ChangeActionUpdate
		summary = fmt.Sprintf("%s修改合同 %s「%s」", prefix, contract.ContractNo, contract.Title)
		changes = s.diffContract(ctx, existing, &contract)
	} else {
		changes = s.contractChanges(ctx, &contract)
		if sourceNo != "" {
			// B7: the audit entry names the source contract number so the
			// reuse (who, when, from which contract) stays traceable. The
			// number comes from buildContract's validation read, so this path
			// cannot silently fall back to a plain create summary.
			summary = fmt.Sprintf("续签（来源 %s）：新建合同 %s「%s」",
				sourceNo, contract.ContractNo, contract.Title)
		}
	}
	s.logChange(ctx, model.ChangeEntityContract, contract.ID, action, summary, changes)
	return contract, warnings, nil
}

// buildContract validates and materializes a contract. dryRun skips contract
// number allocation and party creation so Excel previews have no side effects.
// sourceNo is the contract number of the verified renewal source (empty for
// plain creates and updates) so callers can build the audit summary from the
// same read instead of fetching the source row twice.
func (s *Service) buildContract(ctx context.Context, in ContractInput, existing *model.Contract, dryRun bool) (model.Contract, string, []string, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return model.Contract{}, "", nil, err
	}
	validation := &ValidationErrors{}
	warnings := []string{}
	contract := model.Contract{}
	if existing != nil {
		contract = *existing
		contract.Data = copyData(existing.Data)
	} else {
		contract.Data = map[string]any{}
	}

	// B7: a renewal source must exist when creating. The link is create-only
	// and never copies signatures, seals or documents from the source; a new
	// contract must collect its own signatures before generation.
	sourceNo := ""
	if existing == nil {
		if sourceID := strings.TrimSpace(in.SourceContractID); sourceID != "" {
			source, err := s.rd.GetContract(ctx, sourceID)
			if err != nil {
				if isNotFound(err) {
					return model.Contract{}, "", nil, business("not_found", "续签来源合同不存在")
				}
				return model.Contract{}, "", nil, err
			}
			contract.SourceContractID = sourceID
			sourceNo = source.ContractNo
		}
	}

	// Year first: auto numbers are scoped to a year.
	year := in.Year
	if year == 0 && existing != nil {
		year = existing.Year
	}
	if year == 0 {
		if signed, ok := normalizeDate(in.SignedOn); ok && signed != "" {
			if n, err := strconv.Atoi(signed[:4]); err == nil {
				year = n
			}
		}
	}
	if year == 0 {
		year = s.now().Year()
	}
	if year < 1900 || year > 2200 {
		validation.add(model.FieldYear, "年度", "应在 1900～2200 之间")
	}
	contract.Year = year

	requestedNo := strings.TrimSpace(in.ContractNo)
	switch {
	case existing != nil:
		if requestedNo != "" && requestedNo != existing.ContractNo {
			return model.Contract{}, "", nil, business("immutable",
				"合同编号创建后不可修改（当前编号 %s）", existing.ContractNo)
		}
		contract.ContractNo = existing.ContractNo
	case requestedNo != "":
		if err := validateContractNo(requestedNo); err != nil {
			validation.addErr(model.FieldContractNo, "合同编号", err)
		} else if other, err := s.rd.GetContractByNo(ctx, requestedNo); err == nil {
			return model.Contract{}, "", nil, business("conflict",
				"合同编号「%s」已存在（合同 %s）", requestedNo, other.Title)
		} else if !isNotFound(err) {
			return model.Contract{}, "", nil, err
		}
		contract.ContractNo = requestedNo
	case dryRun:
		contract.ContractNo = ""
	default:
		no, err := s.nextContractNo(ctx, year)
		if err != nil {
			return model.Contract{}, "", nil, err
		}
		contract.ContractNo = no
	}

	contract.Title = truncateRunes(strings.TrimSpace(in.Title), 200)
	if contract.Title == "" {
		validation.add(model.FieldTitle, "合同名称", "不能为空")
	}
	contract.Type = truncateRunes(strings.TrimSpace(in.Type), 100)

	if strings.TrimSpace(in.Status) == "" && existing != nil {
		// Keep the existing status when a partial caller omits it.
	} else if status, ok := normalizeStatus(in.Status); ok {
		contract.Status = status
	} else {
		validation.add(model.FieldStatus, "状态", "只能是 草稿 / 生效 / 归档")
	}
	if contract.Status == "" {
		contract.Status = model.ContractDraft
	}

	for _, item := range []struct {
		field string
		label string
		raw   string
		dst   *string
	}{
		{model.FieldSignedOn, "签订日期", in.SignedOn, &contract.SignedOn},
		{model.FieldValidFrom, "生效日期", in.ValidFrom, &contract.ValidFrom},
		{model.FieldValidTo, "到期日期", in.ValidTo, &contract.ValidTo},
	} {
		value, ok := normalizeDate(item.raw)
		if !ok {
			validation.add(item.field, item.label, "日期格式应为 YYYY-MM-DD")
			continue
		}
		*item.dst = value
	}

	if math.IsNaN(in.Amount) || math.IsInf(in.Amount, 0) || in.Amount < 0 {
		validation.add(model.FieldAmount, "金额", "必须是非负数字")
	} else {
		contract.Amount = math.Round(in.Amount*100) / 100
	}

	if id := strings.TrimSpace(in.PartyAID); id != "" {
		if _, err := s.rd.GetParty(ctx, id); err != nil {
			if isNotFound(err) {
				validation.add("party_a_id", "甲方", "档案不存在")
			} else {
				return model.Contract{}, "", nil, err
			}
		} else {
			contract.PartyAID = id
		}
	}
	if id := strings.TrimSpace(in.TemplateID); id != "" {
		if _, err := s.rd.GetTemplate(ctx, id); err != nil {
			if isNotFound(err) {
				validation.add("template_id", "模板", "不存在")
			} else {
				return model.Contract{}, "", nil, err
			}
		} else {
			contract.TemplateID = id
		}
	}

	switch {
	case strings.TrimSpace(in.PartyBID) != "":
		party, err := s.rd.GetParty(ctx, strings.TrimSpace(in.PartyBID))
		if err != nil {
			if isNotFound(err) {
				validation.add(model.FieldPartyB, "相对方", "档案不存在")
			} else {
				return model.Contract{}, "", nil, err
			}
		} else {
			contract.PartyBID = party.ID
		}
	case strings.TrimSpace(in.PartyB) != "":
		name := strings.TrimSpace(in.PartyB)
		matches, err := s.rd.FindPartiesByName(ctx, name)
		if err != nil {
			return model.Contract{}, "", nil, err
		}
		switch len(matches) {
		case 1:
			contract.PartyBID = matches[0].ID
		case 0:
			if in.CreateParty == nil || *in.CreateParty {
				if dryRun {
					warnings = append(warnings, fmt.Sprintf("将新建相对方档案「%s」", name))
				} else {
					party, err := s.createParty(ctx, name)
					if err != nil {
						return model.Contract{}, "", nil, err
					}
					contract.PartyBID = party.ID
					warnings = append(warnings, fmt.Sprintf("已新建相对方档案「%s」，可在档案页补充信息", name))
				}
			} else {
				validation.add(model.FieldPartyB, "相对方", fmt.Sprintf("档案中不存在「%s」", name))
			}
		default:
			return model.Contract{}, "", nil, business("ambiguous_party",
				"存在 %d 个同名相对方「%s」，请从档案中选择具体记录", len(matches), name)
		}
	case existing != nil && existing.PartyBID != "":
		contract.PartyBID = existing.PartyBID
	default:
		validation.add(model.FieldPartyB, "相对方", "不能为空")
	}

	applyDynamicFieldInput(fields, contract.Data, in.Data, existing == nil, validation)
	if contract.ContractNo == "" && !dryRun {
		validation.add(model.FieldContractNo, "合同编号", "不能为空")
	}
	if err := validation.errOrNil(); err != nil {
		return contract, sourceNo, warnings, err
	}

	if contract.PartyBID != "" && contract.Year > 0 && contract.Title != "" {
		exclude := ""
		if existing != nil {
			exclude = existing.ID
		}
		similar, err := s.rd.FindSimilarContracts(ctx, contract.PartyBID, contract.Year, contract.Title, exclude)
		if err != nil {
			return model.Contract{}, "", nil, err
		}
		for _, other := range similar {
			warnings = append(warnings, fmt.Sprintf(
				"已存在相同年度、相对方和名称的合同（编号 %s），请确认不是重复录入", other.ContractNo))
		}
	}
	return contract, sourceNo, warnings, nil
}

// createParty creates a stub archive record for a counterparty typed during
// contract entry. Required party fields are intentionally not enforced here:
// the archive can be completed later without blocking the contract.
func (s *Service) createParty(ctx context.Context, name string) (model.Party, error) {
	party := model.Party{
		Name: truncateRunes(strings.TrimSpace(name), 120),
		Kind: model.PartyPerson,
		Data: map[string]any{},
	}
	if fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityParty, false); err == nil {
		for _, field := range fields {
			if field.BuiltIn || field.DefaultValue == "" {
				continue
			}
			if value, err := coerceFieldValue(field, field.DefaultValue); err == nil && value != nil {
				party.Data[field.Key] = value
			}
		}
	}
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionCreate,
		fmt.Sprintf("新建相对方档案「%s」（合同录入时自动建立）", party.Name),
		[]model.FieldChange{{Field: model.FieldPartyName, Label: "名称", Old: "", New: party.Name}})
	return party, nil
}

// nextContractNo allocates HT-YYYY-NNNN, skipping numbers already in use.
func (s *Service) nextContractNo(ctx context.Context, year int) (string, error) {
	prefix := fmt.Sprintf("HT-%d-", year)
	maxSuffix, err := s.rd.MaxContractNumberSuffix(ctx, prefix)
	if err != nil {
		return "", err
	}
	for i := maxSuffix + 1; i < maxSuffix+10000; i++ {
		candidate := fmt.Sprintf("%s%04d", prefix, i)
		_, err := s.rd.GetContractByNo(ctx, candidate)
		if errors.Is(err, store.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("无法分配合同编号：%s 段已用尽", prefix)
}

func validateContractNo(s string) error {
	if s == "" {
		return fmt.Errorf("不能为空")
	}
	if len([]rune(s)) > 64 {
		return fmt.Errorf("不能超过 64 个字符")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("不能包含控制字符")
		}
	}
	return nil
}

// DeleteContract removes a contract record and records the deletion.
func (s *Service) DeleteContract(ctx context.Context, id string) error {
	return s.deleteContract(ctx, id, "")
}

// deleteContract removes a contract record and records the deletion. source
// "excel" marks import-driven deletes in the audit summary.
func (s *Service) deleteContract(ctx context.Context, id, source string) error {
	contract, err := s.rd.GetContract(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return business("not_found", "合同不存在")
		}
		return err
	}
	if err := s.st.DeleteContract(ctx, contract.ID); err != nil {
		if isNotFound(err) {
			return business("not_found", "合同不存在")
		}
		return err
	}
	prefix := ""
	if source == "excel" {
		prefix = "Excel 导入："
	}
	s.logChange(ctx, model.ChangeEntityContract, contract.ID, model.ChangeActionDelete,
		fmt.Sprintf("%s删除合同 %s「%s」", prefix, contract.ContractNo, contract.Title), nil)
	return nil
}

// ListContracts is the read surface used by the app API and exports.
func (s *Service) ListContracts(ctx context.Context, f model.ListFilter) (model.List[model.Contract], error) {
	return s.rd.ListContracts(ctx, f)
}

// ContractYears returns per-year counts for the year switcher.
func (s *Service) ContractYears(ctx context.Context) ([]model.YearCount, error) {
	return s.rd.ContractYears(ctx)
}

// ListParties is the read surface used by the app API and exports.
func (s *Service) ListParties(ctx context.Context, f model.ListFilter) (model.List[model.Party], error) {
	return s.rd.ListParties(ctx, f)
}

func (s *Service) diffContract(ctx context.Context, before, after *model.Contract) []model.FieldChange {
	oldName := s.partyName(ctx, before.PartyBID)
	newName := s.partyName(ctx, after.PartyBID)
	return diffValueMaps(
		s.contractValues(ctx, before, oldName),
		s.contractValues(ctx, after, newName),
		s.fieldLabels(ctx, model.FieldEntityContract),
	)
}

func (s *Service) contractChanges(ctx context.Context, contract *model.Contract) []model.FieldChange {
	values := s.contractValues(ctx, contract, s.partyName(ctx, contract.PartyBID))
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, true)
	if err != nil {
		return nil
	}
	changes := []model.FieldChange{}
	for _, field := range fields {
		value := values[field.Key]
		if value == "" {
			continue
		}
		changes = append(changes, model.FieldChange{Field: field.Key, Label: field.Label, Old: "", New: value})
	}
	return changes
}

func (s *Service) contractValues(ctx context.Context, contract *model.Contract, partyBName string) map[string]string {
	values := map[string]string{
		model.FieldContractNo: contract.ContractNo,
		model.FieldYear:       strconv.Itoa(contract.Year),
		model.FieldTitle:      contract.Title,
		model.FieldType:       contract.Type,
		model.FieldPartyB:     partyBName,
		model.FieldAmount:     displayValue(contract.Amount),
		model.FieldSignedOn:   contract.SignedOn,
		model.FieldValidFrom:  contract.ValidFrom,
		model.FieldValidTo:    contract.ValidTo,
		model.FieldStatus:     StatusLabel(contract.Status),
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, true)
	if err != nil {
		return values
	}
	for _, field := range fields {
		if field.BuiltIn {
			continue
		}
		values[field.Key] = displayValue(contract.Data[field.Key])
	}
	return values
}

func (s *Service) partyName(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	party, err := s.rd.GetParty(ctx, id)
	if err != nil {
		return ""
	}
	return party.Name
}
