package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// FileName is the uploaded file name recorded in the import audit.
	FileName string `json:"file_name"`
	// ApplyDeletes applies snapshot rows that are missing from the file. It is
	// off by default so a partially edited workbook never removes data silently.
	ApplyDeletes bool `json:"apply_deletes"`
	// Resolutions maps a preview row key (the Excel row number for file rows,
	// "delete:<contract_id>" for delete candidates) to "keep_system" or
	// "use_excel". Every conflict row needs an explicit choice before commit
	// writes anything.
	Resolutions map[string]string `json:"resolutions"`
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

// ImportRow is one previewed or committed row. Row actions:
//
//	create / update / unchanged / error  -- one Excel data row
//	delete                               -- present at export, missing from the file
//	conflict                             -- needs an explicit keep_system/use_excel choice
type ImportRow struct {
	// Key is the stable resolution key: the row number for file rows and
	// "delete:<contract_id>" for rows missing from the file.
	Key        string `json:"key"`
	Row        int    `json:"row"` // 1-based Excel row; 0 for delete candidates
	ContractID string `json:"contract_id,omitempty"`
	Action     string `json:"action"`
	ContractNo string `json:"contract_no"`
	Title      string `json:"title"`
	PartyB     string `json:"party_b"`
	// SystemValues and ExcelValues show the differing cells of a conflict.
	SystemValues   map[string]string `json:"system_values,omitempty"`
	ExcelValues    map[string]string `json:"excel_values,omitempty"`
	ConflictReason string            `json:"conflict_reason,omitempty"`
	Errors         []FieldError      `json:"errors"`
	Warnings       []string          `json:"warnings"`
}

// ImportSummary counts preview actions.
type ImportSummary struct {
	Total     int `json:"total"`
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Error     int `json:"error"`
	// Deleted counts rows present in the export snapshot but missing from the
	// file. They are applied only when commit passes apply_deletes=true.
	Deleted int `json:"deleted"`
	// Conflicts counts rows (file rows and delete candidates) that need a
	// per-row keep_system/use_excel decision.
	Conflicts int `json:"conflicts"`
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
	Deleted   int         `json:"deleted"`
	Conflicts int         `json:"conflicts"`
	Skipped   int         `json:"skipped"`
	Rows      []ImportRow `json:"rows"`
}

type importPlan struct {
	in       ContractInput
	existing *model.Contract
	contract model.Contract
	warnings []string
	errs     []FieldError
}

// importContext is the parsed workbook plus the resolved field/mapping state
// shared by preview and commit.
type importContext struct {
	fields        []model.FieldDefinition
	byKey         map[string]model.FieldDefinition
	mapping       map[string]string
	mode          string
	createParties *bool
	snapshot      *xlsx.Snapshot
	keys          []string // fingerprint keys (sorted)
	missingKeys   []string // snapshot keys whose field is now deleted/archived
	byID          map[string]xlsx.SnapshotRow
	byNo          map[string]xlsx.SnapshotRow
}

// TemplateWorkbook builds the blank Excel entry template from the current
// field definitions. B14 removed the in_ledger filter: the template and the
// ledger are always the full current field definition, so a field added at
// runtime shows up without any column configuration.
func (s *Service) TemplateWorkbook(ctx context.Context) ([]byte, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return nil, err
	}
	return xlsx.BuildTemplate(fields)
}

// LedgerWorkbook exports contracts using the current field definitions and a
// hidden snapshot that lets PreviewImport detect edits, deletions and
// conflicts when the file comes back.
func (s *Service) LedgerWorkbook(ctx context.Context, filter model.ListFilter) ([]byte, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return nil, err
	}
	contracts, err := pageAll(func(offset int) (model.List[model.Contract], error) {
		f := filter
		f.Limit = 500
		f.Offset = offset
		return s.rd.ListContracts(ctx, f)
	})
	if err != nil {
		return nil, err
	}
	keys := sortedFieldKeys(fields)
	snapshot := &xlsx.Snapshot{
		Version:    xlsx.SnapshotVersion,
		ExportedAt: s.now().UTC().Format(time.RFC3339),
		FieldKeys:  keys,
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
		row := make([]any, len(fields))
		canonical := make(map[string]string, len(fields))
		for j, field := range fields {
			value := excelValue(field, contract, name)
			row[j] = value
			canonical[field.Key] = canonicalExportValue(field, value)
		}
		rows = append(rows, row)
		snapshot.Rows = append(snapshot.Rows, xlsx.SnapshotRow{
			ContractID:  contract.ID,
			ContractNo:  contract.ContractNo,
			Fingerprint: fingerprintValues(keys, canonical),
		})
	}
	return xlsx.BuildLedger(fields, rows, snapshot)
}

func sortedFieldKeys(fields []model.FieldDefinition) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	sort.Strings(keys)
	return keys
}

// canonicalCellValue normalizes one cell for fingerprinting so display noise
// (thousands separators, date spellings, 是/TRUE) never looks like an edit.
func canonicalCellValue(field model.FieldDefinition, raw string) string {
	raw = strings.TrimSpace(raw)
	switch field.Type {
	case model.FieldTypeNumber, model.FieldTypeMoney:
		if v, ok := parseNumber(raw); ok {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	case model.FieldTypeDate:
		if v, ok := normalizeDate(raw); ok {
			return v
		}
	case model.FieldTypeBool:
		if v, ok := parseBoolValue(raw); ok {
			if v {
				return "是"
			}
			return "否"
		}
	}
	return raw
}

func canonicalExportValue(field model.FieldDefinition, value any) string {
	return canonicalCellValue(field, rawString(value))
}

// fingerprintValues hashes field values ordered by key. It is the row
// fingerprint stored in the export snapshot and recomputed on both the Excel
// side and the system side during import.
func fingerprintValues(keys []string, values map[string]string) string {
	ordered := append([]string(nil), keys...)
	sort.Strings(ordered)
	h := sha256.New()
	for _, key := range ordered {
		h.Write([]byte(key))
		h.Write([]byte{0x1f})
		h.Write([]byte(values[key]))
		h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))
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
//
// When the file carries a B14 snapshot the preview also classifies rows as
// update/conflict by comparing three fingerprints: export-time, system now and
// Excel now. Rows missing from the file become delete candidates.
func (s *Service) PreviewImport(ctx context.Context, data []byte, opts ImportOptions) (ImportPreview, error) {
	preview, _, _, err := s.previewPlan(ctx, data, opts)
	return preview, err
}

func (s *Service) previewPlan(ctx context.Context, data []byte, opts ImportOptions) (ImportPreview, *importContext, xlsx.ReadResult, error) {
	parsed, ictx, err := s.buildImportContext(ctx, data, opts)
	if err != nil {
		return ImportPreview{}, nil, xlsx.ReadResult{}, err
	}
	preview := ImportPreview{
		Sheet:   parsed.Sheet,
		Mapping: ictx.mapping,
		Fields:  ictx.fields,
		Rows:    []ImportRow{},
		Notes:   []string{},
	}
	preview.Columns = buildColumns(parsed.Headers, ictx.mapping, ictx.byKey)
	preview.Notes = mappingNotes(parsed.Headers, ictx.mapping, ictx.byKey, ictx.fields)
	seen := map[string]bool{}
	for _, row := range parsed.Rows {
		previewRow, snapshotRow, err := s.previewRow(ctx, row, ictx)
		if err != nil {
			return ImportPreview{}, nil, xlsx.ReadResult{}, err
		}
		if snapshotRow != nil {
			seen[snapshotRow.ContractID] = true
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
		case "conflict":
			preview.Summary.Conflicts++
		default:
			preview.Summary.Error++
		}
	}
	if ictx.snapshot != nil && ictx.mode == "upsert" {
		deletes, err := s.deleteCandidates(ctx, ictx, seen)
		if err != nil {
			return ImportPreview{}, nil, xlsx.ReadResult{}, err
		}
		for _, row := range deletes {
			preview.Rows = append(preview.Rows, row)
			if row.Action == "delete" {
				preview.Summary.Deleted++
			} else {
				preview.Summary.Conflicts++
			}
		}
	}
	switch {
	case ictx.snapshot == nil:
		preview.Notes = append(preview.Notes,
			"文件没有「_腾手快照」工作表（外部或旧版文件），按普通导入处理：不识别删除与冲突。")
	case preview.Summary.Deleted > 0:
		preview.Notes = append(preview.Notes, fmt.Sprintf(
			"Excel 中缺少 %d 行导出时存在的合同；默认不删除，确认后勾选「应用删除」。", preview.Summary.Deleted))
	}
	if len(ictx.missingKeys) > 0 {
		preview.Notes = append(preview.Notes, fmt.Sprintf(
			"导出后字段定义有变化（已删除或归档：%s），旧文件无法准确比较这些列，相关行可能显示为冲突；建议重新导出台账后再编辑。",
			strings.Join(ictx.missingKeys, "、")))
	}
	if preview.Summary.Conflicts > 0 {
		preview.Notes = append(preview.Notes, fmt.Sprintf(
			"有 %d 行冲突（导出后两边都被修改或系统已改动），需逐行选择「保留系统 / 采用 Excel」。", preview.Summary.Conflicts))
	}
	if preview.Summary.Total == 0 {
		preview.Notes = append(preview.Notes, "表格中没有数据行。")
	}
	return preview, ictx, parsed, nil
}

func (s *Service) buildImportContext(ctx context.Context, data []byte, opts ImportOptions) (xlsx.ReadResult, *importContext, error) {
	parsed, err := xlsx.Read(data)
	if err != nil {
		return xlsx.ReadResult{}, nil, business("invalid_excel", "%s", err.Error())
	}
	mode, err := normalizeImportMode(opts.Mode)
	if err != nil {
		return xlsx.ReadResult{}, nil, err
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil {
		return xlsx.ReadResult{}, nil, err
	}
	byKey := map[string]model.FieldDefinition{}
	for _, field := range fields {
		byKey[field.Key] = field
	}
	mapping := resolveMapping(parsed.Headers, byKey, opts.Mapping)
	if len(opts.Mapping) == 0 {
		mapping = suggestMapping(parsed.Headers, fields)
	}
	ictx := &importContext{
		fields:        fields,
		byKey:         byKey,
		mapping:       mapping,
		mode:          mode,
		createParties: opts.CreateParties,
		snapshot:      parsed.Snapshot,
	}
	if parsed.Snapshot != nil {
		ictx.byID = map[string]xlsx.SnapshotRow{}
		ictx.byNo = map[string]xlsx.SnapshotRow{}
		for _, row := range parsed.Snapshot.Rows {
			ictx.byID[row.ContractID] = row
			if row.ContractNo != "" {
				ictx.byNo[row.ContractNo] = row
			}
		}
		ictx.keys = append([]string(nil), parsed.Snapshot.FieldKeys...)
		if len(ictx.keys) == 0 {
			for _, key := range mapping {
				ictx.keys = append(ictx.keys, key)
			}
		}
		sort.Strings(ictx.keys)
		for _, key := range ictx.keys {
			if _, ok := byKey[key]; !ok {
				ictx.missingKeys = append(ictx.missingKeys, key)
			}
		}
	}
	return parsed, ictx, nil
}

// previewRow validates one Excel row and classifies it. The returned snapshot
// row (when any) is the export-time record the row belongs to; callers use it
// to know which snapshot rows are still present in the file.
func (s *Service) previewRow(ctx context.Context, row xlsx.Row, ictx *importContext) (ImportRow, *xlsx.SnapshotRow, error) {
	existing, snapshotRow, err := s.resolveExisting(ctx, row, ictx)
	if err != nil {
		return ImportRow{}, nil, err
	}
	plan, err := s.planImportRow(ctx, row, existing, ictx)
	if err != nil {
		return ImportRow{}, nil, err
	}
	preview := ImportRow{
		Key:      strconv.Itoa(row.Number),
		Row:      row.Number,
		Errors:   plan.errs,
		Warnings: plan.warnings,
	}
	if existing != nil {
		preview.ContractID = existing.ID
	}
	partyName := plan.in.PartyB
	if plan.contract.PartyBID != "" {
		partyName = s.partyName(ctx, plan.contract.PartyBID)
	}
	preview.ContractNo = plan.contract.ContractNo
	if preview.ContractNo == "" {
		preview.ContractNo = "（导入时自动生成）"
	}
	preview.Title = plan.contract.Title
	preview.PartyB = partyName

	switch {
	case len(plan.errs) > 0:
		preview.Action = "error"
	case existing == nil && snapshotRow == nil:
		preview.Action = "create"
	case snapshotRow != nil && existing == nil:
		// The contract existed at export time but the system deleted it since.
		preview.Action = "conflict"
		preview.ContractNo = snapshotRow.ContractNo
		preview.ConflictReason = "系统在导出后删除了该合同；选择「采用 Excel」将按此行重新创建。"
		preview.ExcelValues = s.excelValuesByKey(row, ictx)
	case snapshotRow == nil && ictx.snapshot != nil:
		// The system created this contract after the export, so applying the
		// row would overwrite data the file never saw.
		preview.Action = "conflict"
		preview.ConflictReason = "系统在导出后新建了该编号的合同（导出快照中没有此行）。"
		preview.SystemValues = s.systemValuesByKey(ctx, existing, ictx)
		preview.ExcelValues = s.excelValuesByKey(row, ictx)
	case snapshotRow == nil:
		// External/legacy workbook without a snapshot: keep the B2 upsert
		// behavior and never invent conflicts from missing snapshot rows.
		if len(s.diffContract(ctx, existing, &plan.contract)) == 0 {
			preview.Action = "unchanged"
		} else {
			preview.Action = "update"
		}
	default:
		fpSystem := s.systemFingerprint(ctx, existing, ictx)
		fpExcel := s.excelFingerprint(row, ictx)
		switch {
		case fpSystem != snapshotRow.Fingerprint && fpExcel != snapshotRow.Fingerprint:
			preview.Action = "conflict"
			preview.ConflictReason = "该合同在导出后于 Excel 和系统中都被修改；请选择保留哪一边。"
		case fpSystem != snapshotRow.Fingerprint:
			preview.Action = "conflict"
			preview.ConflictReason = "系统在导出后修改了该合同；采用 Excel 会覆盖系统改动。"
		case fpExcel != snapshotRow.Fingerprint:
			preview.Action = "update"
		default:
			if len(s.diffContract(ctx, existing, &plan.contract)) == 0 {
				preview.Action = "unchanged"
			} else {
				preview.Action = "update"
			}
		}
		if preview.Action == "conflict" {
			system, excel := s.conflictValues(ctx, existing, row, ictx)
			preview.SystemValues = system
			preview.ExcelValues = excel
		}
	}
	return preview, snapshotRow, nil
}

// resolveExisting finds the system record an Excel row refers to. Priority:
// the hidden contract-id column, then the contract number recorded in the
// export snapshot (this survives both Excel edits and future renumbering),
// then the current system contract number for external files.
func (s *Service) resolveExisting(ctx context.Context, row xlsx.Row, ictx *importContext) (*model.Contract, *xlsx.SnapshotRow, error) {
	id := strings.TrimSpace(row.ContractID)
	number := cellByKey(row, ictx.mapping, model.FieldContractNo)
	// An id recorded in the snapshot tells the caller this row belonged to a
	// contract at export time even when the current system no longer has it.
	var knownSnapshot *xlsx.SnapshotRow
	if id != "" {
		if snapshot, ok := ictx.byID[id]; ok {
			knownSnapshot = &snapshot
		}
		contract, err := s.rd.GetContract(ctx, id)
		if err == nil {
			return &contract, snapshotRowByID(ictx, contract.ID), nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, nil, err
		}
	}
	if ictx.snapshot != nil && number != "" {
		if snapshot, ok := ictx.byNo[number]; ok {
			contract, err := s.rd.GetContract(ctx, snapshot.ContractID)
			if err == nil {
				return &contract, &snapshot, nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return nil, nil, err
			}
			// The snapshot id is missing here: the contract was deleted, or
			// the file came from another data directory. If the number still
			// resolves, surface it as a conflict against that record instead
			// of failing the row.
			if other, err := s.rd.GetContractByNo(ctx, number); err == nil {
				return &other, nil, nil
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, nil, err
			}
			return nil, &snapshot, nil
		}
	}
	if number != "" {
		contract, err := s.rd.GetContractByNo(ctx, number)
		if err == nil {
			return &contract, snapshotRowByID(ictx, contract.ID), nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, nil, err
		}
	}
	return nil, knownSnapshot, nil
}

func snapshotRowByID(ictx *importContext, id string) *xlsx.SnapshotRow {
	if ictx.snapshot == nil || id == "" {
		return nil
	}
	if snapshot, ok := ictx.byID[id]; ok {
		return &snapshot
	}
	return nil
}

func cellByKey(row xlsx.Row, mapping map[string]string, key string) string {
	for idxStr, mapped := range mapping {
		if mapped != key {
			continue
		}
		idx, _ := strconv.Atoi(idxStr)
		if idx >= 0 && idx < len(row.Cells) {
			return strings.TrimSpace(row.Cells[idx])
		}
	}
	return ""
}

// excelValuesByKey returns the canonical Excel-side values for every snapshot
// key (empty when the column is missing or unmapped).
func (s *Service) excelValuesByKey(row xlsx.Row, ictx *importContext) map[string]string {
	raw := map[string]string{}
	for idxStr, key := range ictx.mapping {
		field, ok := ictx.byKey[key]
		if !ok {
			continue
		}
		idx, _ := strconv.Atoi(idxStr)
		if idx >= 0 && idx < len(row.Cells) {
			raw[key] = canonicalCellValue(field, row.Cells[idx])
		}
	}
	values := make(map[string]string, len(ictx.keys))
	for _, key := range ictx.keys {
		if _, ok := ictx.byKey[key]; !ok {
			continue
		}
		values[key] = raw[key]
	}
	return values
}

func (s *Service) systemValuesByKey(ctx context.Context, contract *model.Contract, ictx *importContext) map[string]string {
	if contract == nil {
		return map[string]string{}
	}
	all := s.contractValues(ctx, contract, s.partyName(ctx, contract.PartyBID))
	values := make(map[string]string, len(ictx.keys))
	for _, key := range ictx.keys {
		if _, ok := ictx.byKey[key]; !ok {
			continue
		}
		values[key] = canonicalCellValue(ictx.byKey[key], all[key])
	}
	return values
}

func (s *Service) conflictValues(ctx context.Context, existing *model.Contract, row xlsx.Row, ictx *importContext) (map[string]string, map[string]string) {
	system := s.systemValuesByKey(ctx, existing, ictx)
	excel := s.excelValuesByKey(row, ictx)
	diffSystem := map[string]string{}
	diffExcel := map[string]string{}
	for _, key := range ictx.keys {
		if _, ok := ictx.byKey[key]; !ok {
			continue
		}
		if system[key] == excel[key] {
			continue
		}
		diffSystem[key] = system[key]
		diffExcel[key] = excel[key]
	}
	if len(diffSystem) == 0 {
		return system, excel
	}
	return diffSystem, diffExcel
}

func (s *Service) systemFingerprint(ctx context.Context, contract *model.Contract, ictx *importContext) string {
	return fingerprintValues(ictx.keys, s.systemValuesByKey(ctx, contract, ictx))
}

func (s *Service) excelFingerprint(row xlsx.Row, ictx *importContext) string {
	return fingerprintValues(ictx.keys, s.excelValuesByKey(row, ictx))
}

// deleteCandidates lists contracts recorded in the snapshot that no longer
// appear in the file. A candidate whose system row changed since the export is
// a conflict: deleting it would discard the system-side edit.
func (s *Service) deleteCandidates(ctx context.Context, ictx *importContext, seen map[string]bool) ([]ImportRow, error) {
	out := []ImportRow{}
	for _, snapshot := range ictx.snapshot.Rows {
		if seen[snapshot.ContractID] {
			continue
		}
		contract, err := s.rd.GetContract(ctx, snapshot.ContractID)
		if errors.Is(err, store.ErrNotFound) {
			continue // already gone from the system: nothing to delete
		}
		if err != nil {
			return nil, err
		}
		row := ImportRow{
			Key:        "delete:" + contract.ID,
			Action:     "delete",
			ContractID: contract.ID,
			ContractNo: contract.ContractNo,
			Title:      contract.Title,
			PartyB:     s.partyName(ctx, contract.PartyBID),
			Errors:     []FieldError{},
			Warnings:   []string{},
		}
		if s.systemFingerprint(ctx, &contract, ictx) != snapshot.Fingerprint {
			row.Action = "conflict"
			row.ConflictReason = "系统在导出后修改了该合同；勾选「应用删除」后需逐行确认，默认保留系统。"
			row.SystemValues = s.systemValuesByKey(ctx, &contract, ictx)
		}
		out = append(out, row)
	}
	return out, nil
}

// CommitImport writes the rows that pass validation. With SkipErrors=false a
// single invalid row blocks the whole import and no writes happen. Conflicts
// require an explicit per-row resolution; deletes require apply_deletes.
func (s *Service) CommitImport(ctx context.Context, data []byte, opts ImportOptions) (ImportResult, error) {
	preview, ictx, parsed, err := s.previewPlan(ctx, data, opts)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{Rows: preview.Rows}
	if preview.Summary.Error > 0 && !opts.SkipErrors {
		result.Errors = preview.Summary.Error
		return result, business("import_errors",
			"有 %d 行存在错误，未写入任何数据；请修正后重新导入，或勾选「跳过错误行」", preview.Summary.Error)
	}
	// Validate conflict choices up front so a missing decision never leads to
	// partial writes.
	for _, row := range preview.Rows {
		if row.Action != "conflict" {
			continue
		}
		if row.Row == 0 && !opts.ApplyDeletes {
			continue // deletion is off; nothing to decide
		}
		if normalizeResolution(opts.Resolutions[row.Key]) == "" {
			return result, business("conflict_unresolved",
				"有冲突行尚未选择处理方式；请逐行选择「保留系统」或「采用 Excel」后再提交")
		}
	}
	parsedByNumber := make(map[int]xlsx.Row, len(parsed.Rows))
	for _, row := range parsed.Rows {
		parsedByNumber[row.Number] = row
	}

	for i := range result.Rows {
		row := &result.Rows[i]
		switch row.Action {
		case "unchanged":
			result.Unchanged++
			continue
		case "error":
			result.Errors++
			continue
		case "conflict":
			if row.Row == 0 {
				if err := s.commitDeleteConflict(ctx, row, opts, &result); err != nil {
					return result, err
				}
				continue
			}
			result.Conflicts++
			if normalizeResolution(opts.Resolutions[row.Key]) == "keep_system" {
				result.Skipped++
				continue
			}
			// use_excel: continue after the switch and write the Excel values.
		case "delete":
			if err := s.commitDelete(ctx, ictx, row, opts, &result); err != nil {
				return result, err
			}
			continue
		}
		xlsxRow, ok := parsedByNumber[row.Row]
		if !ok {
			markRowError(row, "导入", "该行在文件中已不存在")
			result.Errors++
			continue
		}
		existing, _, err := s.resolveExisting(ctx, xlsxRow, ictx)
		if err != nil {
			markRowError(row, "导入", err.Error())
			result.Errors++
			continue
		}
		plan, err := s.planImportRow(ctx, xlsxRow, existing, ictx)
		if err != nil {
			markRowError(row, "导入", err.Error())
			result.Errors++
			continue
		}
		if len(plan.errs) > 0 {
			row.Action = "error"
			row.Errors = plan.errs
			result.Errors++
			continue
		}
		plan.in.Source = "excel" // audit prefix on the per-contract change log
		saved, warnings, err := s.SaveContract(ctx, plan.in)
		if err != nil {
			var verrs *ValidationErrors
			if errors.As(err, &verrs) && len(verrs.Items) > 0 {
				row.Errors = verrs.Items
			} else {
				row.Errors = []FieldError{{Label: "导入", Message: err.Error()}}
			}
			row.Action = "error"
			result.Errors++
			continue
		}
		row.ContractNo = saved.ContractNo
		row.Title = saved.Title
		row.Warnings = append(row.Warnings, warnings...)
		if existing == nil {
			row.Action = "create"
			result.Created++
		} else {
			row.Action = "update"
			result.Updated++
		}
	}
	s.auditImport(ctx, opts, preview.Sheet, data, result)
	return result, nil
}

func (s *Service) commitDelete(ctx context.Context, ictx *importContext, row *ImportRow, opts ImportOptions, result *ImportResult) error {
	if !opts.ApplyDeletes {
		result.Skipped++
		return nil
	}
	contract, err := s.rd.GetContract(ctx, row.ContractID)
	if errors.Is(err, store.ErrNotFound) {
		result.Skipped++
		return nil
	}
	if err != nil {
		return err
	}
	snapshot, ok := ictx.byID[contract.ID]
	if !ok || snapshot.Fingerprint != s.systemFingerprint(ctx, &contract, ictx) {
		// Re-verify right before deleting: an unchanged snapshot no longer
		// matches the system, so the delete becomes a conflict instead of
		// silently discarding the system change.
		row.Action = "conflict"
		row.ConflictReason = "提交时发现系统已修改该合同，未删除；请重新预览后再确认。"
		result.Conflicts++
		return nil
	}
	if err := s.deleteContract(ctx, contract.ID, "excel"); err != nil {
		if isNotFound(err) {
			result.Skipped++
			return nil
		}
		return err
	}
	result.Deleted++
	return nil
}

func (s *Service) commitDeleteConflict(ctx context.Context, row *ImportRow, opts ImportOptions, result *ImportResult) error {
	result.Conflicts++
	if !opts.ApplyDeletes || normalizeResolution(opts.Resolutions[row.Key]) != "use_excel" {
		result.Skipped++
		return nil
	}
	if err := s.deleteContract(ctx, row.ContractID, "excel"); err != nil {
		if isNotFound(err) {
			result.Skipped++
			return nil
		}
		return err
	}
	row.Action = "delete"
	result.Deleted++
	return nil
}

func markRowError(row *ImportRow, label, message string) {
	row.Action = "error"
	row.Errors = append(row.Errors, FieldError{Label: label, Message: message})
}

func normalizeResolution(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "keep_system", "keep", "system":
		return "keep_system"
	case "use_excel", "excel", "apply":
		return "use_excel"
	}
	return ""
}

// auditImport appends one import-level audit entry with the file name, sheet,
// change counts and file digest. Per-row audits come from SaveContract /
// deleteContract with Source="excel".
func (s *Service) auditImport(ctx context.Context, opts ImportOptions, sheet string, data []byte, result ImportResult) {
	sum := sha256.Sum256(data)
	fileSHA := hex.EncodeToString(sum[:])
	name := strings.TrimSpace(opts.FileName)
	if name == "" {
		name = "未命名文件"
	}
	summary := fmt.Sprintf(
		"Excel 导入「%s」·工作表 %s：新增 %d、更新 %d、不变 %d、删除 %d、跳过 %d、冲突 %d、错误 %d；文件 SHA-256 %s",
		name, sheet, result.Created, result.Updated, result.Unchanged, result.Deleted,
		result.Skipped, result.Conflicts, result.Errors, fileSHA)
	s.logChange(ctx, model.ChangeEntityImport, fileSHA, model.ChangeActionImport, summary, nil)
}

func (s *Service) planImportRow(ctx context.Context, row xlsx.Row, existing *model.Contract, ictx *importContext) (importPlan, error) {
	plan := importPlan{existing: existing}
	rawByKey := map[string]string{}
	for idxStr, key := range ictx.mapping {
		idx, _ := strconv.Atoi(idxStr)
		if idx >= 0 && idx < len(row.Cells) {
			rawByKey[key] = strings.TrimSpace(row.Cells[idx])
		}
	}
	if existing != nil && ictx.mode == "create" {
		plan.errs = append(plan.errs, FieldError{
			Field: model.FieldContractNo, Label: "合同编号",
			Message: "该编号已存在；当前为「仅新增」模式，不覆盖已有合同",
		})
	}
	existingName := ""
	if existing != nil {
		existingName = s.partyName(ctx, existing.PartyBID)
		plan.in = contractInputFromExisting(existing, existingName)
		// Orphaned data keys (fields removed or archived since the value was
		// written) stay in the stored record but must not be resubmitted:
		// buildContract rejects values for undefined fields, which would make
		// every round-trip of such a contract fail.
		for key := range plan.in.Data {
			if field, ok := ictx.byKey[key]; !ok || field.BuiltIn {
				delete(plan.in.Data, key)
			}
		}
	} else {
		plan.in = ContractInput{Data: map[string]any{}}
	}
	plan.in.CreateParty = ictx.createParties
	idxKeys := make([]string, 0, len(ictx.mapping))
	for idxStr := range ictx.mapping {
		idxKeys = append(idxKeys, idxStr)
	}
	sort.Slice(idxKeys, func(i, j int) bool {
		a, _ := strconv.Atoi(idxKeys[i])
		b, _ := strconv.Atoi(idxKeys[j])
		return a < b
	})
	var immutableNo string
	for _, idxStr := range idxKeys {
		field, ok := ictx.byKey[ictx.mapping[idxStr]]
		if !ok {
			continue
		}
		idx, _ := strconv.Atoi(idxStr)
		cell := ""
		if idx >= 0 && idx < len(row.Cells) {
			cell = strings.TrimSpace(row.Cells[idx])
		}
		if existing != nil && cell == "" {
			continue // empty cells leave existing values unchanged
		}
		if existing != nil && field.Key == model.FieldContractNo && cell != existing.ContractNo {
			// Contract numbers are immutable (B2). Keep the system number and
			// warn instead of failing the whole row: the hidden contract id
			// already matched this record, so the other edits are still valid.
			immutableNo = existing.ContractNo
			continue
		}
		if err := applyRawField(&plan.in, field, cell, existingName); err != nil {
			plan.errs = append(plan.errs, FieldError{Field: field.Key, Label: field.Label, Message: err.Error()})
		}
	}
	contract, _, warnings, err := s.buildContract(ctx, plan.in, plan.existing, true)
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
	if immutableNo != "" {
		plan.warnings = append(plan.warnings,
			fmt.Sprintf("合同编号创建后不可修改，已保留原编号 %s。", immutableNo))
	}
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
	used := map[string]bool{}
	indexes := make([]int, 0, len(provided))
	for idxStr := range provided {
		if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(headers) {
			indexes = append(indexes, idx)
		}
	}
	// Leftmost column wins when a caller maps several columns to one field, so
	// previews and fingerprints stay deterministic.
	sort.Ints(indexes)
	for _, idx := range indexes {
		key := strings.TrimSpace(provided[strconv.Itoa(idx)])
		if _, ok := byKey[key]; !ok || used[key] {
			continue
		}
		out[strconv.Itoa(idx)] = key
		used[key] = true
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
