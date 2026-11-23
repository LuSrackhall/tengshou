// Package xlsx builds and reads the Excel ledger workbooks used as a first
// class data-entry channel. Column sets are always derived from the current
// field definitions; there are no hard-coded ledger headers.
package xlsx

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// Sheet names used by generated workbooks.
const (
	SheetLedger = "合同台账"
	SheetHelp   = "填写说明"
)

// B14 round-trip technical layout. Exported ledgers carry a hidden trailing
// column with the stable contract id and a hidden snapshot worksheet with the
// export-time state of every row. Both are skipped by Read's data-sheet scan.
const (
	// SheetSnapshot is the hidden worksheet recording the export state.
	SheetSnapshot = "_腾手快照"
	// SnapshotVersion is the current snapshot layout version.
	SnapshotVersion = 1
	// ColumnContractID is the hidden trailing ledger column carrying contract ids.
	ColumnContractID = "_合同ID"
	// technicalPrefix marks technical worksheets that are never data sheets.
	technicalPrefix = "_腾手"
)

// SnapshotRow is one contract's state at export time.
type SnapshotRow struct {
	ContractID  string
	ContractNo  string
	Fingerprint string
}

// Snapshot is the export-time state of a ledger workbook. A file without one
// is treated as external/legacy: no delete or conflict detection.
type Snapshot struct {
	Version    int
	ExportedAt string
	FieldKeys  []string
	Rows       []SnapshotRow
}

// Row is one parsed worksheet row. Number is the 1-based Excel row number so
// validation reports point at what the user sees in Excel.
type Row struct {
	Number int
	Cells  []string
	// ContractID is taken from the hidden trailing technical column when the
	// file was exported by BuildLedger. Empty for externally created files.
	ContractID string
}

// ReadResult is a parsed workbook.
type ReadResult struct {
	Sheet    string
	Headers  []string
	Rows     []Row
	Snapshot *Snapshot
}

// Read parses the first usable worksheet of an .xlsx workbook. The first
// non-empty row becomes the header row. Technical worksheets (names starting
// with "_腾手", including the hidden snapshot sheet) are never chosen as the
// data sheet. Old binary .xls files are rejected by excelize; callers should
// surface that error as a request to save as .xlsx.
func Read(data []byte) (ReadResult, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return ReadResult{}, fmt.Errorf("无法读取 Excel 文件（请使用 .xlsx 格式）: %w", err)
	}
	defer f.Close()
	var sheet string
	for _, name := range f.GetSheetList() {
		if name == SheetHelp || strings.HasPrefix(name, technicalPrefix) {
			continue
		}
		rows, err := f.GetRows(name)
		if err != nil {
			return ReadResult{}, err
		}
		if !allEmpty(rows) {
			sheet = name
			break
		}
	}
	if sheet == "" {
		return ReadResult{}, fmt.Errorf("Excel 文件中没有可用的数据表")
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		return ReadResult{}, err
	}
	result := ReadResult{Sheet: sheet, Snapshot: readSnapshot(f)}
	headerRow := -1
	for i, cells := range rows {
		if !emptyCells(cells) {
			headerRow = i
			result.Headers = normalizeWidth(cells, len(cells))
			for j := range result.Headers {
				result.Headers[j] = strings.TrimSpace(result.Headers[j])
			}
			break
		}
	}
	if headerRow < 0 {
		return ReadResult{}, fmt.Errorf("Excel 文件中没有表头行")
	}
	hasIDColumn := len(result.Headers) > 0 &&
		result.Headers[len(result.Headers)-1] == ColumnContractID
	if hasIDColumn {
		result.Headers = result.Headers[:len(result.Headers)-1]
	}
	width := len(result.Headers)
	for i := headerRow + 1; i < len(rows); i++ {
		cells := rows[i]
		total := width
		if hasIDColumn {
			total = width + 1
		}
		full := normalizeWidth(cells, total)
		contractID := ""
		if hasIDColumn {
			contractID = strings.TrimSpace(full[width])
		}
		visible := full[:width]
		if emptyCells(visible) {
			// A row whose visible cells are all blank counts as missing even
			// when the hidden id column still has a value: the user removed
			// the row's data, so B14 proposes a delete instead of an update.
			continue
		}
		if len(result.Rows) >= 10000 {
			return ReadResult{}, fmt.Errorf("Excel 数据行超过 10000 行，请拆分后再导入")
		}
		result.Rows = append(result.Rows, Row{
			Number:     i + 1,
			Cells:      visible,
			ContractID: contractID,
		})
	}
	return result, nil
}

// readSnapshot parses the hidden snapshot worksheet. A malformed or missing
// sheet yields nil, which callers treat as an external/legacy file.
func readSnapshot(f *excelize.File) *Snapshot {
	found := false
	for _, name := range f.GetSheetList() {
		if name == SheetSnapshot {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	rows, err := f.GetRows(SheetSnapshot)
	if err != nil || len(rows) < 2 {
		return nil
	}
	header := rows[0]
	if len(header) < 6 || strings.TrimSpace(header[0]) != "快照版本" {
		return nil
	}
	meta := rows[1]
	if len(meta) < 3 {
		return nil
	}
	version, err := strconv.Atoi(strings.TrimSpace(meta[0]))
	if err != nil || version != SnapshotVersion {
		return nil
	}
	snapshot := &Snapshot{
		Version:    version,
		ExportedAt: strings.TrimSpace(meta[1]),
	}
	if keys := strings.TrimSpace(meta[2]); keys != "" {
		for _, key := range strings.Split(keys, ",") {
			if key = strings.TrimSpace(key); key != "" {
				snapshot.FieldKeys = append(snapshot.FieldKeys, key)
			}
		}
	}
	for _, row := range rows[2:] {
		if len(row) < 6 {
			continue
		}
		id := strings.TrimSpace(row[3])
		fingerprint := strings.TrimSpace(row[5])
		if id == "" || fingerprint == "" {
			continue
		}
		snapshot.Rows = append(snapshot.Rows, SnapshotRow{
			ContractID:  id,
			ContractNo:  strings.TrimSpace(row[4]),
			Fingerprint: fingerprint,
		})
	}
	return snapshot
}

func normalizeWidth(cells []string, width int) []string {
	out := make([]string, width)
	for i := 0; i < width && i < len(cells); i++ {
		out[i] = cells[i]
	}
	return out
}

func emptyCells(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func allEmpty(rows [][]string) bool {
	for _, r := range rows {
		if !emptyCells(r) {
			return false
		}
	}
	return true
}

// BuildTemplate creates the blank entry workbook: one header row derived from
// fields, validated select/bool cells and a help sheet describing each column.
func BuildTemplate(fields []model.FieldDefinition) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	index, err := f.NewSheet(SheetLedger)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(index)
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return nil, err
	}
	if err := writeHeader(f, SheetLedger, fields); err != nil {
		return nil, err
	}
	if err := writeValidations(f, SheetLedger, fields); err != nil {
		return nil, err
	}
	if err := writeHelpSheet(f, fields); err != nil {
		return nil, err
	}
	f.SetDocProps(&excelize.DocProperties{
		Title:   "腾手合同台账录入模板",
		Creator: "腾手",
	})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildLedger creates an export workbook from the same field definitions.
// Cell values are either string or float64; numeric values stay numeric so
// users can sum them in Excel. It also writes the hidden contract-id column
// and, when snapshot is non-nil, the hidden snapshot worksheet that enables
// round-trip delete/conflict detection. Image assets are never embedded.
//
// rows and snapshot.Rows must be in the same order and have the same length:
// snapshot.Rows[i] supplies the contract id written for rows[i]. Any mismatch
// would silently put a contract id on the wrong row, so it is rejected.
func BuildLedger(fields []model.FieldDefinition, rows [][]any, snapshot *Snapshot) ([]byte, error) {
	if snapshot != nil && len(snapshot.Rows) != len(rows) {
		return nil, fmt.Errorf("台账行数与快照行数不一致（%d 行 vs %d 行快照），无法对应合同 ID", len(rows), len(snapshot.Rows))
	}
	f := excelize.NewFile()
	defer f.Close()
	index, err := f.NewSheet(SheetLedger)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(index)
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return nil, err
	}
	if err := writeHeader(f, SheetLedger, fields); err != nil {
		return nil, err
	}
	idCol, err := excelize.ColumnNumberToName(len(fields) + 1)
	if err != nil {
		return nil, err
	}
	if err := f.SetCellValue(SheetLedger, idCol+"1", ColumnContractID); err != nil {
		return nil, err
	}
	if err := f.SetColVisible(SheetLedger, idCol, false); err != nil {
		return nil, err
	}
	moneyStyle := moneyStyleID(f)
	for i, row := range rows {
		excelRow := i + 2
		for j := range fields {
			if j >= len(row) || row[j] == nil {
				continue
			}
			cell, err := excelize.CoordinatesToCellName(j+1, excelRow)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellValue(SheetLedger, cell, row[j]); err != nil {
				return nil, err
			}
			if fields[j].Type == model.FieldTypeMoney && moneyStyle != 0 {
				_ = f.SetCellStyle(SheetLedger, cell, cell, moneyStyle)
			}
		}
		if snapshot != nil && i < len(snapshot.Rows) {
			cell, err := excelize.CoordinatesToCellName(len(fields)+1, excelRow)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellValue(SheetLedger, cell, snapshot.Rows[i].ContractID); err != nil {
				return nil, err
			}
		}
	}
	lastCol, _ := excelize.ColumnNumberToName(len(fields))
	_ = f.AutoFilter(SheetLedger, "A1:"+lastCol+"1", nil)
	if snapshot != nil {
		if err := writeSnapshotSheet(f, snapshot); err != nil {
			return nil, err
		}
	}
	f.SetDocProps(&excelize.DocProperties{
		Title:   "腾手合同台账",
		Creator: "腾手",
	})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeSnapshotSheet writes the hidden snapshot worksheet. Layout:
//
//	row 1: 快照版本 | 导出时间 | 字段键 | 合同ID | 合同编号 | 行指纹
//	row 2: version  | RFC3339  | key,key… |         |          |
//	row 3+:                                    id |  no      | sha256
func writeSnapshotSheet(f *excelize.File, snapshot *Snapshot) error {
	if _, err := f.NewSheet(SheetSnapshot); err != nil {
		return err
	}
	headers := []string{"快照版本", "导出时间", "字段键", "合同ID", "合同编号", "行指纹"}
	style := headerStyle(f)
	for i, header := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(SheetSnapshot, cell, header); err != nil {
			return err
		}
		_ = f.SetCellStyle(SheetSnapshot, cell, cell, style)
	}
	meta := []any{snapshot.Version, snapshot.ExportedAt, strings.Join(snapshot.FieldKeys, ",")}
	for i, value := range meta {
		cell, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(SheetSnapshot, cell, value); err != nil {
			return err
		}
	}
	for i, row := range snapshot.Rows {
		excelRow := i + 3
		values := []any{"", "", "", row.ContractID, row.ContractNo, row.Fingerprint}
		for j, value := range values {
			cell, err := excelize.CoordinatesToCellName(j+1, excelRow)
			if err != nil {
				return err
			}
			if err := f.SetCellValue(SheetSnapshot, cell, value); err != nil {
				return err
			}
		}
	}
	_ = f.SetColWidth(SheetSnapshot, "C", "C", 48)
	_ = f.SetColWidth(SheetSnapshot, "F", "F", 72)
	return f.SetSheetVisible(SheetSnapshot, false)
}

func moneyStyleID(f *excelize.File) int {
	id, err := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	if err != nil {
		return 0
	}
	return id
}

func writeHeader(f *excelize.File, sheet string, fields []model.FieldDefinition) error {
	for i, field := range fields {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, field.Label); err != nil {
			return err
		}
		if err := f.SetCellStyle(sheet, cell, cell, headerStyle(f)); err != nil {
			return err
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		width := columnWidth(field)
		_ = f.SetColWidth(sheet, col, col, width)
	}
	_ = f.SetRowHeight(sheet, 1, 28)
	return f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
}

func headerStyle(f *excelize.File) int {
	id, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F6F4A"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "D0D7DE", Style: 1},
			{Type: "right", Color: "D0D7DE", Style: 1},
			{Type: "top", Color: "D0D7DE", Style: 1},
			{Type: "bottom", Color: "D0D7DE", Style: 1},
		},
	})
	if err != nil {
		return 0
	}
	return id
}

func columnWidth(field model.FieldDefinition) float64 {
	width := float64(len([]rune(field.Label))*2 + 4)
	switch field.Type {
	case model.FieldTypeLongText:
		width = 42
	case model.FieldTypeDate:
		width = 14
	case model.FieldTypeMoney, model.FieldTypeNumber:
		width = 14
	}
	if width < 10 {
		width = 10
	}
	if width > 48 {
		width = 48
	}
	return width
}

func writeValidations(f *excelize.File, sheet string, fields []model.FieldDefinition) error {
	for i, field := range fields {
		col, _ := excelize.ColumnNumberToName(i + 1)
		var options []string
		switch field.Type {
		case model.FieldTypeSelect:
			options = field.Options
		case model.FieldTypeBool:
			options = []string{"是", "否"}
		default:
			continue
		}
		if len(options) == 0 {
			continue
		}
		sqref := col + "2:" + col + "5000"
		dv := excelize.NewDataValidation(true)
		dv.Sqref = sqref
		dv.SetDropList(options)
		dv.SetError(excelize.DataValidationErrorStyleStop, "取值不在允许范围内", "请选择下拉列表中的值")
		dv.SetInput("可选值", strings.Join(options, " / "))
		if err := f.AddDataValidation(sheet, dv); err != nil {
			return err
		}
	}
	return nil
}

func writeHelpSheet(f *excelize.File, fields []model.FieldDefinition) error {
	if _, err := f.NewSheet(SheetHelp); err != nil {
		return err
	}
	headers := []string{"列名", "字段键", "类型", "必填", "默认值", "说明"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(SheetHelp, cell, h)
		_ = f.SetCellStyle(SheetHelp, cell, cell, headerStyle(f))
	}
	_ = f.SetColWidth(SheetHelp, "A", "A", 16)
	_ = f.SetColWidth(SheetHelp, "B", "B", 20)
	_ = f.SetColWidth(SheetHelp, "C", "C", 12)
	_ = f.SetColWidth(SheetHelp, "D", "D", 8)
	_ = f.SetColWidth(SheetHelp, "E", "E", 12)
	_ = f.SetColWidth(SheetHelp, "F", "F", 52)
	for i, field := range fields {
		row := i + 2
		values := []any{
			field.Label, field.Key, TypeLabel(field.Type), yesNo(field.Required),
			field.DefaultValue, field.Note,
		}
		for j, v := range values {
			cell, _ := excelize.CoordinatesToCellName(j+1, row)
			_ = f.SetCellValue(SheetHelp, cell, v)
		}
	}
	noteRow := len(fields) + 3
	_ = f.SetCellValue(SheetHelp, fmt.Sprintf("A%d", noteRow),
		"填写提示：金额填数字；日期填 YYYY-MM-DD；下拉列请使用下拉选择。空白单元格在导入时表示“不填写/保持不变”。")
	return f.MergeCell(SheetHelp, fmt.Sprintf("A%d", noteRow), fmt.Sprintf("F%d", noteRow))
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

// TypeLabel returns the Chinese label for a field type.
func TypeLabel(t string) string {
	switch t {
	case model.FieldTypeText:
		return "文本"
	case model.FieldTypeLongText:
		return "长文本"
	case model.FieldTypeNumber:
		return "数字"
	case model.FieldTypeMoney:
		return "金额"
	case model.FieldTypeDate:
		return "日期"
	case model.FieldTypeSelect:
		return "单选"
	case model.FieldTypeBool:
		return "是/否"
	case model.FieldTypeImage:
		return "图片路径"
	case model.FieldTypeParty:
		return "相对方"
	case model.FieldTypeStatus:
		return "状态"
	default:
		return t
	}
}
