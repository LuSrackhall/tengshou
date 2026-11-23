package xlsx_test

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/xlsx"
)

func testFields() []model.FieldDefinition {
	return []model.FieldDefinition{
		{Key: "contract_no", Label: "合同编号", Type: model.FieldTypeText},
		{Key: "amount", Label: "金额（元）", Type: model.FieldTypeMoney},
		{Key: "usage", Label: "用途", Type: model.FieldTypeText},
	}
}

// B14: a ledger carries the hidden contract-id column and the hidden snapshot
// sheet, and Read parses both back without exposing the technical column as a
// user field.
func TestLedgerSnapshotRoundTrip(t *testing.T) {
	fields := testFields()
	snapshot := &xlsx.Snapshot{
		Version:    xlsx.SnapshotVersion,
		ExportedAt: "2026-11-23T00:00:00Z",
		FieldKeys:  []string{"amount", "contract_no", "usage"},
		Rows: []xlsx.SnapshotRow{
			{ContractID: "ctr_a", ContractNo: "HT-2026-0001", Fingerprint: strings.Repeat("a", 64)},
			{ContractID: "ctr_b", ContractNo: "HT-2026-0002", Fingerprint: strings.Repeat("b", 64)},
		},
	}
	rows := [][]any{
		{"HT-2026-0001", 1200.5, "居住"},
		{"HT-2026-0002", 0.0, ""},
	}
	data, err := xlsx.BuildLedger(fields, rows, snapshot)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := xlsx.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Sheet != xlsx.SheetLedger {
		t.Fatalf("sheet = %q", parsed.Sheet)
	}
	if len(parsed.Headers) != len(fields) {
		t.Fatalf("headers = %v; technical column must not leak", parsed.Headers)
	}
	for i, field := range fields {
		if parsed.Headers[i] != field.Label {
			t.Fatalf("header %d = %q, want %q", i, parsed.Headers[i], field.Label)
		}
	}
	if len(parsed.Rows) != 2 {
		t.Fatalf("rows = %+v", parsed.Rows)
	}
	if parsed.Rows[0].ContractID != "ctr_a" || parsed.Rows[1].ContractID != "ctr_b" {
		t.Fatalf("row contract ids = %q, %q", parsed.Rows[0].ContractID, parsed.Rows[1].ContractID)
	}
	if parsed.Rows[0].Cells[0] != "HT-2026-0001" || parsed.Rows[0].Cells[2] != "居住" {
		t.Fatalf("row cells = %+v", parsed.Rows[0].Cells)
	}

	if parsed.Snapshot == nil {
		t.Fatal("snapshot not parsed")
	}
	if parsed.Snapshot.Version != xlsx.SnapshotVersion ||
		parsed.Snapshot.ExportedAt != snapshot.ExportedAt {
		t.Fatalf("snapshot meta = %+v", parsed.Snapshot)
	}
	if got := strings.Join(parsed.Snapshot.FieldKeys, ","); got != "amount,contract_no,usage" {
		t.Fatalf("snapshot field keys = %q", got)
	}
	if len(parsed.Snapshot.Rows) != 2 ||
		parsed.Snapshot.Rows[0].ContractID != "ctr_a" ||
		parsed.Snapshot.Rows[0].Fingerprint != snapshot.Rows[0].Fingerprint {
		t.Fatalf("snapshot rows = %+v", parsed.Snapshot.Rows)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if visible, err := f.GetSheetVisible(xlsx.SheetSnapshot); err != nil || visible {
		t.Fatalf("snapshot sheet visible=%v err=%v", visible, err)
	}
	idCol, _ := excelize.ColumnNumberToName(len(fields) + 1)
	if visible, err := f.GetColVisible(xlsx.SheetLedger, idCol); err != nil || visible {
		t.Fatalf("id column %s visible=%v err=%v", idCol, visible, err)
	}

	// Signature/seal assets are never embedded: the workbook must not contain
	// any media part.
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range zr.File {
		if strings.Contains(strings.ToLower(file.Name), "media") {
			t.Fatalf("workbook embeds media: %s", file.Name)
		}
	}
}

// BuildLedger relies on rows and snapshot.Rows being in lockstep; a mismatch
// must fail loudly instead of writing contract ids onto the wrong rows.
func TestBuildLedgerRejectsSnapshotLengthMismatch(t *testing.T) {
	rows := [][]any{
		{"HT-2026-0001", 1200.5, "居住"},
		{"HT-2026-0002", 0.0, ""},
	}
	snapshot := &xlsx.Snapshot{
		Version:    xlsx.SnapshotVersion,
		ExportedAt: "2026-11-23T00:00:00Z",
		FieldKeys:  []string{"amount", "contract_no", "usage"},
		Rows: []xlsx.SnapshotRow{
			{ContractID: "ctr_a", ContractNo: "HT-2026-0001", Fingerprint: strings.Repeat("a", 64)},
		},
	}
	if _, err := xlsx.BuildLedger(testFields(), rows, snapshot); err == nil {
		t.Fatal("BuildLedger should reject rows/snapshot length mismatch")
	}
}

// Files without a snapshot are external/legacy: Read must not invent one and
// must not treat them as round-trip exports.
func TestReadWorkbookWithoutSnapshot(t *testing.T) {
	template, err := xlsx.BuildTemplate(testFields())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := xlsx.Read(template)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Snapshot != nil {
		t.Fatalf("template must not carry a snapshot: %+v", parsed.Snapshot)
	}
	for _, row := range parsed.Rows {
		if row.ContractID != "" {
			t.Fatalf("template row has contract id %q", row.ContractID)
		}
	}
}

// Technical worksheets are skipped even when they hold data.
func TestReadSkipsTechnicalSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	if _, err := f.NewSheet("_腾手说明"); err != nil {
		t.Fatal(err)
	}
	_ = f.SetCellValue("_腾手说明", "A1", "technical")
	sheet := "合同台账"
	if _, err := f.NewSheet(sheet); err != nil {
		t.Fatal(err)
	}
	_ = f.DeleteSheet("Sheet1")
	_ = f.SetCellValue(sheet, "A1", "合同名称")
	_ = f.SetCellValue(sheet, "A2", "测试合同")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := xlsx.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Sheet != sheet || len(parsed.Rows) != 1 || parsed.Rows[0].Cells[0] != "测试合同" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Snapshot != nil {
		t.Fatal("no snapshot expected")
	}
}

// A malformed snapshot sheet is ignored instead of failing the import.
func TestReadIgnoresMalformedSnapshot(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "合同台账"
	if _, err := f.NewSheet(sheet); err != nil {
		t.Fatal(err)
	}
	_ = f.DeleteSheet("Sheet1")
	_ = f.SetCellValue(sheet, "A1", "合同名称")
	_ = f.SetCellValue(sheet, "A2", "测试合同")
	if _, err := f.NewSheet(xlsx.SheetSnapshot); err != nil {
		t.Fatal(err)
	}
	_ = f.SetCellValue(xlsx.SheetSnapshot, "A1", "随便写的")
	_ = f.SetSheetVisible(xlsx.SheetSnapshot, false)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := xlsx.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Snapshot != nil {
		t.Fatalf("malformed snapshot should be ignored: %+v", parsed.Snapshot)
	}
}
