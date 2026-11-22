package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
	"github.com/LuSrackhall/tengshou/internal/xlsx"
)

// ImportOptions controls preview and commit of an Excel entry file.
type ImportOptions struct {
	// Mapping is column index (as a string) -> field key. Empty means the
	// service suggests a mapping from the header row.
	Mapping map[string]string `json:"mapping"`
	// Mode is "upsert" (default: existing contract numbers are updated) or
	// "create" (existing numbers are errors).
	Mode string `json:"mode"`
	// SkipErrors imports the valid rows and reports the invalid ones.
	SkipErrors bool `json:"skip_errors"`
	// CreateParties controls whether unknown counterparty names create an
	// archive record. Default true.
	CreateParties *bool `json:"create_parties"`
}

// ImportColumn describes one header column and its resolved field.
type ImportColumn struct {
	Index    int    `json:"index"`
	Header   string `json:"header"`
	Field    string `json:"field,omitempty"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required"`
}

// ImportRow is one previewed or committed row.
type ImportRow struct {
	Row        int               `json:"row"`
	Action     string            `json:"action"` // create / update / unchanged / error
	ContractNo string            `json:"contract_no"`
	Title      string            `json:"title"`
	PartyB     string            `json:"party_b"`
	Values     map[string]string `json:"values"`
	Errors     []FieldError      `json:"errors"`
	Warnings   []string          `json:"warnings"`
}

// ImportSummary counts preview actions.
type ImportSummary struct {
	Total     int `json:"total"`
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Error     int `json:"error"`
}

// ImportPreview is the side-effect-free parse result shown to the user.
type ImportPreview struct {
	Sheet   string                  `json:"sheet"`
	Columns []ImportColumn          `json:"columns"`
	Mapping map[string]string       `json:"mapping"`
	Rows    []ImportRow             `json:"rows"`
	Summary ImportSummary           `json:"summary"`
	Fields  []model.FieldDefinition `json:"fields"`
	Notes   []string                `json:"notes"`
}

// ImportResult is returned by CommitImport.
type ImportResult struct {
	Created   int         `json:"created"`
	Updated   int         `json:"updated"`
	Unchanged int         `json:"unchanged"`
	Errors    int         `json:"errors"`
	Rows      []ImportRow `json:"rows"`
}

type importPlan struct {
	in       ContractInput
	existing *model.Contract
	contract model.Contract
	warnings []string
	errs     []FieldError
}

// TemplateWorkbook builds the blank Excel entry template from the current
// field definitions. Required fields are included even if they are not marked
// for the ledger, because a usable entry form must collect them.
func (s *Service) TemplateWorkbook(ctx context.Context) ([]byte, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return nil, err
	}
	return xlsx.BuildTemplate(entryFields(fields))
}

// LedgerWorkbook exports contracts using ledger-visible field definitions.
func (s *Service) LedgerWorkbook(ctx context.Context, filter model.ListFilter) ([]byte, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return nil, err
	}
	columns := ledgerFields(fields)
	contracts, err := pageAll(func(offset int) (model.List[model.Contract], error) {
		f := filter
		f.Limit = 500
		f.Offset = offset
		return s.rd.ListContracts(ctx, f)
	})
	if err != nil {
		return nil, err
	}
	partyNames := map[string]string{}
	rows := make([][]any, 0, len(contracts))
	for i := range contracts {
		contract := &contracts[i]
		name := ""
		if contract.PartyBID != "" {
			if cached, ok := partyNames[contract.PartyBID]; ok {
				name = cached
			} else {
				name = s.partyName(ctx, contract.PartyBID)
				partyNames[contract.PartyBID] = name
			}
		}
		row := make([]any, len(columns))
		for j, field := range columns {
			row[j] = excelValue(field, contract, name)
		}
		rows = append(rows, row)
	}
	return xlsx.BuildLedger(columns, rows)
}

func entryFields(fields []model.FieldDefinition) []model.FieldDefinition {
	out := make([]model.FieldDefinition, 0, len(fields))
	for _, f := range fields {
		if f.InLedger || f.Required {
			out = append(out, f)
		}
	}
	return out
}

func ledgerFields(fields []model.FieldDefinition) []model.FieldDefinition {
	out := make([]model.FieldDefinition, 0, len(fields))
	for _, f := range fields {
		if f.InLedger {
			out = append(out, f)
		}
	}
	return out
}

func excelValue(field model.FieldDefinition, contract *model.Contract, partyName string) any {
	switch field.Key {
	case model.FieldContractNo:
		return contract.ContractNo
	case model.FieldYear:
		return contract.Year
	case model.FieldTitle:
		return contract.Title
	case model.FieldType:
		return contract.Type
	case model.FieldPartyB:
		return partyName
	case model.FieldAmount:
		return contract.Amount
	case model.FieldSignedOn:
		return contract.SignedOn
	case model.FieldValidFrom:
		return contract.ValidFrom
	case model.FieldValidTo:
		return contract.ValidTo
	case model.FieldStatus:
		return StatusLabel(contract.Status)
	}
	value := contract.Data[field.Key]
	switch field.Type {
	case model.FieldTypeNumber, model.FieldTypeMoney:
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return v
		case string:
			if n, ok := parseNumber(v); ok {
				return n
			}
		}
	case model.FieldTypeBool:
		if v, ok := value.(bool); ok {
			if v {
				return "是"
			}
			return "否"
		}
	}
	return displayValue(value)
}

// PreviewImport parses an Excel file, suggests or applies a column mapping,
// validates every row and reports what would happen. It never writes.
func (s *Service) PreviewImport(ctx context.Context, data []byte, opts ImportOptions) (ImportPreview, error) {
	mode, err := normalizeImportMode(opts.Mode)
	if err != nil {
		return ImportPreview{}, err
	}
	parsed, err := xlsx.Read(data)
	if err != nil {
		return ImportPreview{}, business("invalid_excel", "%s", err.Error())
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return ImportPreview{}, err
	}
	byKey := map[string]model.FieldDefinition{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	mapping := resolveMapping(parsed.Headers, byKey, opts.Mapping)
	if len(opts.Mapping) == 0 {
		mapping = suggestMapping(parsed.Headers, fields)
	}
	preview := ImportPreview{
		Sheet:   parsed.Sheet,
		Mapping: mapping,
		Fields:  fields,
		Rows:    []ImportRow{},
		Notes:   []string{},
	}
	preview.Columns = buildColumns(parsed.Headers, mapping, byKey)
	preview.Notes = mappingNotes(parsed.Headers, mapping, byKey, fields)
	for _, row := range parsed.Rows {
		previewRow, err := s.previewRow(ctx, row, mapping, byKey, mode, opts.CreateParties)
		if err != nil {
			return ImportPreview{}, err
		}
		preview.Rows = append(preview.Rows, previewRow)
		preview.Summary.Total++
		switch previewRow.Action {
		case "create":
			preview.Summary.Create++
		case "update":
			preview.Summary.Update++
		case "unchanged":
			preview.Summary.Unchanged++
		default:
			preview.Summary.Error++
		}
	}
	if preview.Summary.Total == 0 {
		preview.Notes = append(preview.Notes, "表格中没有数据行。")
	}
	return preview, nil
}

// CommitImport writes the rows that pass validation. With SkipErrors=false a
// single invalid row blocks the whole import and no writes happen.
func (s *Service) CommitImport(ctx context.Context, data []byte, opts ImportOptions) (ImportResult, error) {
	preview, err := s.PreviewImport(ctx, data, opts)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{Rows: preview.Rows}
	if preview.Summary.Error > 0 && !opts.SkipErrors {
		result.Errors = preview.Summary.Error
		return result, business("import_errors",
			"有 %d 行存在错误，未写入任何数据；请修正后重新导入，或勾选「跳过错误行」", preview.Summary.Error)
	}
	parsed, err := xlsx.Read(data)
	if err != nil {
		return ImportResult{}, business("invalid_excel", "%s", err.Error())
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return ImportResult{}, err
	}
	byKey := map[string]model.FieldDefinition{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	mapping := resolveMapping(parsed.Headers, byKey, opts.Mapping)
	if len(opts.Mapping) == 0 {
		mapping = suggestMapping(parsed.Headers, fields)
	}
	mode, _ := normalizeImportMode(opts.Mode)
	for i, row := range preview.Rows {
		if i >= len(parsed.Rows) {
			break
		}
		switch row.Action {
		case "unchanged":
			result.Unchanged++
			continue
		case "error":
			result.Errors++
			continue
		}
		plan, err := s.planImportRow(ctx, parsed.Rows[i], mapping, byKey, mode, opts.CreateParties)
		if err != nil {
			result.Rows[i].Action = "error"
			result.Rows[i].Errors = append(result.Rows[i].Errors,
				FieldError{Label: "导入", Message: err.Error()})
			result.Errors++
			continue
		}
		if len(plan.errs) > 0 {
			result.Rows[i].Action = "error"
			result.Rows[i].Errors = plan.errs
			result.Errors++
			continue
		}
		saved, warnings, err := s.SaveContract(ctx, plan.in)
		if err != nil {
			rowErr := FieldError{Label: "导入", Message: err.Error()}
			var verrs *ValidationErrors
			if errors.As(err, &verrs) && len(verrs.Items) > 0 {
				result.Rows[i].Errors = verrs.Items
			} else {
				result.Rows[i].Errors = []FieldError{rowErr}
			}
			result.Rows[i].Action = "error"
			result.Errors++
			continue
		}
		result.Rows[i].ContractNo = saved.ContractNo
		result.Rows[i].Title = saved.Title
		result.Rows[i].Warnings = append(result.Rows[i].Warnings, warnings...)
		if plan.existing == nil {
			result.Created++
		} else {
			result.Updated++
		}
	}
	return result, nil
}

func (s *Service) previewRow(ctx context.Context, row xlsx.Row, mapping map[string]string,
	byKey map[string]model.FieldDefinition, mode string, createParties *bool) (ImportRow, error) {
	plan, err := s.planImportRow(ctx, row, mapping, byKey, mode, createParties)
	if err != nil {
		return ImportRow{}, err
	}
	preview := ImportRow{
		Row:      row.Number,
		Values:   map[string]string{},
		Errors:   plan.errs,
		Warnings: plan.warnings,
	}
	if len(plan.errs) > 0 {
		preview.Action = "error"
	} else if plan.existing == nil {
		preview.Action = "create"
	} else if len(s.diffContract(ctx, plan.existing, &plan.contract)) == 0 {
		preview.Action = "unchanged"
	} else {
		preview.Action = "update"
	}
	partyName := plan.in.PartyB
	if plan.contract.PartyBID != "" {
		partyName = s.partyName(ctx, plan.contract.PartyBID)
	}
	values := s.contractValues(ctx, &plan.contract, partyName)
	for _, key := range mapping {
		if value, ok := values[key]; ok {
			preview.Values[key] = value
		}
	}
	preview.ContractNo = plan.contract.ContractNo
	if preview.ContractNo == "" {
		preview.ContractNo = "（导入时自动生成）"
	}
	preview.Title = plan.contract.Title
	preview.PartyB = partyName
	return preview, nil
}

func (s *Service) planImportRow(ctx context.Context, row xlsx.Row, mapping map[string]string,
	byKey map[string]model.FieldDefinition, mode string, createParties *bool) (importPlan, error) {
	plan := importPlan{}
	rawByKey := map[string]string{}
	for idxStr, key := range mapping {
		idx, _ := strconv.Atoi(idxStr)
		if idx >= 0 && idx < len(row.Cells) {
			rawByKey[key] = strings.TrimSpace(row.Cells[idx])
		}
	}
	if number := rawByKey[model.FieldContractNo]; number != "" {
		existing, err := s.rd.GetContractByNo(ctx, number)
		if err == nil {
			plan.existing = &existing
		} else if !errors.Is(err, store.ErrNotFound) {
			return plan, err
		}
	}
	if plan.existing != nil && mode == "create" {
		plan.errs = append(plan.errs, FieldError{
			Field: model.FieldContractNo, Label: "合同编号",
			Message: "该编号已存在；当前为「仅新增」模式，不覆盖已有合同",
		})
	}
	existingName := ""
	if plan.existing != nil {
		existingName = s.partyName(ctx, plan.existing.PartyBID)
		plan.in = contractInputFromExisting(plan.existing, existingName)
	} else {
		plan.in = ContractInput{Data: map[string]any{}}
	}
	plan.in.CreateParty = createParties
	idxKeys := make([]string, 0, len(mapping))
	for idxStr := range mapping {
		idxKeys = append(idxKeys, idxStr)
	}
	sort.Slice(idxKeys, func(i, j int) bool {
		a, _ := strconv.Atoi(idxKeys[i])
		b, _ := strconv.Atoi(idxKeys[j])
		return a < b
	})
	for _, idxStr := range idxKeys {
		field, ok := byKey[mapping[idxStr]]
		if !ok {
			continue
		}
		idx, _ := strconv.Atoi(idxStr)
		cell := ""
		if idx >= 0 && idx < len(row.Cells) {
			cell = strings.TrimSpace(row.Cells[idx])
		}
		if plan.existing != nil && cell == "" {
			continue // empty cells leave existing values unchanged
		}
		if err := applyRawField(&plan.in, field, cell, existingName); err != nil {
			plan.errs = append(plan.errs, FieldError{Field: field.Key, Label: field.Label, Message: err.Error()})
		}
	}
	contract, warnings, err := s.buildContract(ctx, plan.in, plan.existing, true)
	if err != nil {
		var verrs *ValidationErrors
		if errors.As(err, &verrs) {
			plan.errs = append(plan.errs, verrs.Items...)
		} else {
			var be *Error
			if errors.As(err, &be) {
				plan.errs = append(plan.errs, FieldError{Label: "导入", Message: be.Message})
			} else {
				return plan, err
			}
		}
	}
	plan.contract = contract
	plan.warnings = warnings
	return plan, nil
}

func applyRawField(in *ContractInput, field model.FieldDefinition, raw, existingName string) error {
	raw = strings.TrimSpace(raw)
	switch field.Key {
	case model.FieldContractNo:
		in.ContractNo = raw
		return nil
	case model.FieldYear:
		if raw == "" {
			return nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("必须是整数年度")
		}
		in.Year = n
		return nil
	case model.FieldTitle:
		in.Title = raw
		return nil
	case model.FieldType:
		in.Type = raw
		return nil
	case model.FieldPartyB:
		in.PartyB = raw
		if existingName == "" || raw != existingName {
			in.PartyBID = ""
		}
		return nil
	case model.FieldAmount:
		if raw == "" {
			return nil
		}
		v, ok := parseNumber(raw)
		if !ok {
			return fmt.Errorf("必须是金额数字")
		}
		in.Amount = v
		return nil
	case model.FieldSignedOn:
		in.SignedOn = raw
		return nil
	case model.FieldValidFrom:
		in.ValidFrom = raw
		return nil
	case model.FieldValidTo:
		in.ValidTo = raw
		return nil
	case model.FieldStatus:
		in.Status = raw
		return nil
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}
	in.Data[field.Key] = raw
	return nil
}

func contractInputFromExisting(c *model.Contract, partyName string) ContractInput {
	return ContractInput{
		ID:         c.ID,
		ContractNo: c.ContractNo,
		Year:       c.Year,
		Title:      c.Title,
		Type:       c.Type,
		PartyAID:   c.PartyAID,
		PartyBID:   c.PartyBID,
		PartyB:     partyName,
		Status:     StatusLabel(c.Status),
		SignedOn:   c.SignedOn,
		ValidFrom:  c.ValidFrom,
		ValidTo:    c.ValidTo,
		Amount:     c.Amount,
		TemplateID: c.TemplateID,
		Data:       copyData(c.Data),
	}
}

func normalizeImportMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "upsert", "merge":
		return "upsert", nil
	case "create", "create_only", "append":
		return "create", nil
	default:
		return "", business("invalid_mode", "未知的导入模式 %q", mode)
	}
}

func resolveMapping(headers []string, byKey map[string]model.FieldDefinition, provided map[string]string) map[string]string {
	out := map[string]string{}
	for idxStr, key := range provided {
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= len(headers) {
			continue
		}
		key = strings.TrimSpace(key)
		if _, ok := byKey[key]; !ok {
			continue
		}
		out[strconv.Itoa(idx)] = key
	}
	return out
}

func suggestMapping(headers []string, fields []model.FieldDefinition) map[string]string {
	used := map[string]bool{}
	out := map[string]string{}
	for i, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		found := ""
		for _, field := range fields {
			if !used[field.Key] && strings.EqualFold(strings.TrimSpace(field.Label), header) {
				found = field.Key
				break
			}
		}
		if found == "" {
			for _, field := range fields {
				if !used[field.Key] && strings.EqualFold(field.Key, header) {
					found = field.Key
					break
				}
			}
		}
		if found != "" {
			out[strconv.Itoa(i)] = found
			used[found] = true
		}
	}
	return out
}

func buildColumns(headers []string, mapping map[string]string, byKey map[string]model.FieldDefinition) []ImportColumn {
	columns := make([]ImportColumn, 0, len(headers))
	for i, header := range headers {
		column := ImportColumn{Index: i, Header: strings.TrimSpace(header)}
		if key, ok := mapping[strconv.Itoa(i)]; ok {
			if field, ok := byKey[key]; ok {
				column.Field = field.Key
				column.Label = field.Label
				column.Type = field.Type
				column.Required = field.Required
			}
		}
		columns = append(columns, column)
	}
	return columns
}

func mappingNotes(headers []string, mapping map[string]string, byKey map[string]model.FieldDefinition, fields []model.FieldDefinition) []string {
	notes := []string{}
	mapped := map[string]bool{}
	for _, key := range mapping {
		mapped[key] = true
	}
	for i, header := range headers {
		if strings.TrimSpace(header) == "" {
			continue
		}
		if _, ok := mapping[strconv.Itoa(i)]; !ok {
			notes = append(notes, fmt.Sprintf("第 %d 列「%s」未映射，导入时忽略。", i+1, strings.TrimSpace(header)))
		}
	}
	// contract_no and year are allocated automatically when absent.
	for _, field := range fields {
		if !field.Required || mapped[field.Key] || field.DefaultValue != "" {
			continue
		}
		if field.Key == model.FieldContractNo || field.Key == model.FieldYear {
			continue
		}
		notes = append(notes, fmt.Sprintf("必填列「%s」未映射，新增行会校验失败。", field.Label))
	}
	return notes
}
