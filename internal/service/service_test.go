package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func openService(t *testing.T) (*service.Service, *store.Reader, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rd, err := st.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return service.New(st, rd, "测试员"), rd, st
}

func mustParty(t *testing.T, svc *service.Service, name string) model.Party {
	t.Helper()
	party, _, err := svc.SaveParty(context.Background(), service.PartyInput{
		Kind: model.PartyPerson, Name: name, Phone: "13800000000",
	})
	if err != nil {
		t.Fatalf("save party: %v", err)
	}
	return party
}

func mustContract(t *testing.T, svc *service.Service, in service.ContractInput) model.Contract {
	t.Helper()
	contract, _, err := svc.SaveContract(context.Background(), in)
	if err != nil {
		t.Fatalf("save contract: %v", err)
	}
	return contract
}

func boolPtr(v bool) *bool { return &v }

func TestRuntimeFieldLifecycle(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")

	// A field created at runtime is immediately usable by entry forms.
	field, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "area_sqm", Label: "平方数",
		Type: model.FieldTypeNumber, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create field: %v", err)
	}
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "房屋租赁合同", PartyBID: party.ID, Amount: 1200,
		Data: map[string]any{"area_sqm": "88.5"},
	})
	if got := contract.Data["area_sqm"]; got != 88.5 {
		t.Fatalf("area_sqm = %#v, want 88.5", got)
	}

	// Renaming and making the field required keeps old data untouched.
	if _, err := svc.UpdateField(ctx, field.ID, service.FieldInput{
		Label: "建筑面积", Required: boolPtr(true), InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatalf("update field: %v", err)
	}
	stored, err := rd.GetContract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Data["area_sqm"] != 88.5 {
		t.Fatalf("old value lost after field update: %#v", stored.Data)
	}

	// Updating the contract without re-sending the field keeps the old value,
	// and the now-required field is satisfied by it.
	updated := mustContract(t, svc, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: "房屋租赁合同（修订）", PartyBID: party.ID,
	})
	if updated.Data["area_sqm"] != 88.5 {
		t.Fatalf("merge of unspecified dynamic value failed: %#v", updated.Data)
	}

	// Archiving hides the field from lists/forms but keeps stored values.
	if _, err := svc.ArchiveField(ctx, field.ID); err != nil {
		t.Fatalf("archive field: %v", err)
	}
	active, err := svc.ListFields(ctx, model.FieldEntityContract, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range active {
		if f.Key == "area_sqm" {
			t.Fatal("archived field still listed as active")
		}
	}
	stored, _ = rd.GetContract(ctx, contract.ID)
	if stored.Data["area_sqm"] != 88.5 {
		t.Fatal("archiving a field must not delete stored values")
	}
}

func TestContractNumberingAndImmutability(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")

	first := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "合同一", PartyBID: party.ID, SignedOn: "2026/1/2",
	})
	if first.ContractNo != "HT-2026-0001" {
		t.Fatalf("first number = %q", first.ContractNo)
	}
	if first.SignedOn != "2026-01-02" {
		t.Fatalf("date normalization = %q", first.SignedOn)
	}
	second := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "合同二", PartyBID: party.ID,
	})
	if second.ContractNo != "HT-2026-0002" {
		t.Fatalf("second number = %q", second.ContractNo)
	}
	derived := mustContract(t, svc, service.ContractInput{
		Title: "合同三（按签订日期推年度）", PartyBID: party.ID, SignedOn: "2025-06-01",
	})
	if derived.Year != 2025 || !strings.HasPrefix(derived.ContractNo, "HT-2025-") {
		t.Fatalf("derived year/number = %d %q", derived.Year, derived.ContractNo)
	}

	// The number is immutable.
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: first.ID, ContractNo: "HT-2026-9999", Year: 2026, Title: first.Title, PartyBID: party.ID,
	}); err == nil {
		t.Fatal("expected immutable contract number error")
	}
	// Duplicate numbers are rejected.
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		ContractNo: first.ContractNo, Year: 2026, Title: "重复编号", PartyBID: party.ID,
	}); err == nil {
		t.Fatal("expected duplicate contract number error")
	}
	// Required and typed validation.
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{Year: 2026, PartyBID: party.ID}); err == nil {
		t.Fatal("expected title required error")
	}
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "负金额", PartyBID: party.ID, Amount: -1,
	}); err == nil {
		t.Fatal("expected negative amount error")
	}
	// The stored number did not change.
	stored, err := rd.GetContract(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ContractNo != "HT-2026-0001" {
		t.Fatalf("stored number = %q", stored.ContractNo)
	}
}

func TestValidationChangeLogAndDeletion(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "租赁合同", PartyBID: party.ID, Amount: 1000, Status: "active",
	})
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: "租赁合同", PartyBID: party.ID, Amount: 2000, Status: "active",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntityContract, EntityID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 2 {
		t.Fatalf("change log total = %d, want 2", logs.Total)
	}
	foundAmount := false
	for _, entry := range logs.Items {
		if entry.Actor != "测试员" || entry.CreatedAt.IsZero() {
			t.Fatalf("entry actor/time = %+v", entry)
		}
		for _, change := range entry.Changes {
			if change.Field == "amount" && change.Old == "1000" && change.New == "2000" {
				foundAmount = true
			}
		}
	}
	if !foundAmount {
		t.Fatalf("amount diff missing: %+v", logs.Items)
	}
	if err := svc.DeleteParty(ctx, party.ID); err == nil {
		t.Fatal("deleting a referenced party should fail")
	}
	if err := svc.DeleteContract(ctx, contract.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteParty(ctx, party.ID); err != nil {
		t.Fatalf("delete unreferenced party: %v", err)
	}
}

func TestDuplicateWarning(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	mustContract(t, svc, service.ContractInput{Year: 2026, Title: "同一合同", PartyBID: party.ID})
	second, warnings, err := svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "同一合同", PartyBID: party.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ContractNo == "" || len(warnings) == 0 {
		t.Fatalf("expected duplicate warning, got %v", warnings)
	}
}

func TestCrossYearQueryAndYearMove(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	c2025 := mustContract(t, svc, service.ContractInput{Year: 2025, Title: "2025 年合同", PartyBID: party.ID})
	c2026 := mustContract(t, svc, service.ContractInput{Year: 2026, Title: "2026 年合同", PartyBID: party.ID})

	all, err := rd.ListContracts(ctx, model.ListFilter{})
	if err != nil || all.Total != 2 {
		t.Fatalf("all years = %+v err=%v", all, err)
	}
	only2026, err := rd.ListContracts(ctx, model.ListFilter{Year: 2026})
	if err != nil || only2026.Total != 1 || only2026.Items[0].ID != c2026.ID {
		t.Fatalf("2026 view = %+v err=%v", only2026, err)
	}
	years, err := rd.ContractYears(ctx)
	if err != nil || len(years) != 2 || years[0].Year != 2026 || years[1].Year != 2025 {
		t.Fatalf("years = %+v err=%v", years, err)
	}
	// Moving a contract to another year keeps the record and its number.
	moved := mustContract(t, svc, service.ContractInput{
		ID: c2025.ID, Year: 2027, Title: "2025 年合同", PartyBID: party.ID,
	})
	if moved.ContractNo != c2025.ContractNo || moved.Year != 2027 {
		t.Fatalf("moved contract = %+v", moved)
	}
}

func TestExcelTemplateImportAndExport(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "area_sqm", Label: "平方数",
		Type: model.FieldTypeNumber, Required: boolPtr(true),
		InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	template, err := svc.TemplateWorkbook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	header := readHeader(t, template)
	for _, label := range []string{"合同编号", "年度", "合同名称", "相对方", "金额（元）", "平方数"} {
		if columnIndex(header, label) < 0 {
			t.Fatalf("template header missing %q: %v", label, header)
		}
	}

	// Row 2 is valid; row 3 has an invalid amount.
	workbook := editWorkbook(t, template, func(f *excelize.File, header []string) {
		setCell(t, f, header, "年度", 2, "2026")
		setCell(t, f, header, "合同名称", 2, "导入合同 A")
		setCell(t, f, header, "相对方", 2, "李四")
		setCell(t, f, header, "金额（元）", 2, "1,200.50")
		setCell(t, f, header, "平方数", 2, "88")
		setCell(t, f, header, "年度", 3, "2025")
		setCell(t, f, header, "合同名称", 3, "导入合同 B")
		setCell(t, f, header, "相对方", 3, "王五")
		setCell(t, f, header, "金额（元）", 3, "abc")
		setCell(t, f, header, "平方数", 3, "60")
	})

	preview, err := svc.PreviewImport(ctx, workbook, service.ImportOptions{})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Summary.Total != 2 || preview.Summary.Create != 1 || preview.Summary.Error != 1 {
		t.Fatalf("preview summary = %+v rows=%+v", preview.Summary, preview.Rows)
	}
	if _, err := svc.CommitImport(ctx, workbook, service.ImportOptions{}); err == nil {
		t.Fatal("commit without skip_errors should block")
	}
	if stats, _ := rd.Stats(ctx); stats.Contracts != 0 {
		t.Fatalf("blocked import wrote contracts: %+v", stats)
	}

	result, err := svc.CommitImport(ctx, workbook, service.ImportOptions{SkipErrors: true})
	if err != nil {
		t.Fatalf("commit skip errors: %v", err)
	}
	if result.Created != 1 || result.Errors != 1 {
		t.Fatalf("commit result = %+v", result)
	}
	contracts, err := rd.ListContracts(ctx, model.ListFilter{})
	if err != nil || contracts.Total != 1 {
		t.Fatalf("contracts = %+v err=%v", contracts, err)
	}
	imported := contracts.Items[0]
	if imported.Amount != 1200.5 || imported.Data["area_sqm"] != 88.0 {
		t.Fatalf("imported contract = %+v", imported)
	}
	party, err := rd.GetParty(ctx, imported.PartyBID)
	if err != nil || party.Name != "李四" {
		t.Fatalf("auto-created party = %+v err=%v", party, err)
	}

	// A blank contract number is allocated on commit.
	if !strings.HasPrefix(imported.ContractNo, "HT-2026-") {
		t.Fatalf("allocated number = %q", imported.ContractNo)
	}

	// The exported ledger uses the same dynamic headers and round-trips.
	ledger, err := svc.LedgerWorkbook(ctx, model.ListFilter{Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	exportedHeader := readHeader(t, ledger)
	if columnIndex(exportedHeader, "平方数") < 0 {
		t.Fatalf("ledger header = %v", exportedHeader)
	}
	if got := readCell(t, ledger, exportedHeader, 2, "平方数"); got != "88" {
		t.Fatalf("ledger value = %q", got)
	}
	roundTrip, err := svc.PreviewImport(ctx, ledger, service.ImportOptions{})
	if err != nil {
		t.Fatalf("round-trip preview: %v", err)
	}
	if roundTrip.Summary.Unchanged != 1 {
		// Exported display values must not create spurious diffs.
		t.Fatalf("round-trip summary = %+v rows=%+v", roundTrip.Summary, roundTrip.Rows)
	}
}

func TestExcelUpdateMergeAndMapping(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "usage", Label: "用途",
		Type: model.FieldTypeText, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	contract := mustContract(t, svc, service.ContractInput{
		ContractNo: "CUSTOM-1", Year: 2026, Title: "原合同", PartyBID: party.ID,
		Amount: 100, Data: map[string]any{"usage": "居住"},
	})

	ledger, err := svc.LedgerWorkbook(ctx, model.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	workbook := editWorkbook(t, ledger, func(f *excelize.File, header []string) {
		setCell(t, f, header, "金额（元）", 2, "250.75")
		setCell(t, f, header, "用途", 2, "") // blank leaves the old value
	})
	preview, err := svc.PreviewImport(ctx, workbook, service.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary.Update != 1 || preview.Rows[0].Action != "update" {
		t.Fatalf("preview = %+v", preview.Rows)
	}
	if _, err := svc.CommitImport(ctx, workbook, service.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	updated, err := rd.GetContract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Amount != 250.75 || updated.Data["usage"] != "居住" {
		t.Fatalf("merge result = %+v", updated)
	}

	// Explicit column mapping lets users bring their own header names.
	custom := newWorkbook(t, []string{"名称", "乙方", "年", "钱", "面积"}, [][]string{
		{"自定义表头合同", "李四", "2025", "99.9", "12"},
	})
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "area_sqm", Label: "平方数",
		Type: model.FieldTypeNumber, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	mapping := map[string]string{"0": "title", "1": "party_b", "2": "year", "3": "amount", "4": "area_sqm"}
	preview, err = svc.PreviewImport(ctx, custom, service.ImportOptions{Mapping: mapping})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary.Create != 1 || preview.Rows[0].Title != "自定义表头合同" {
		t.Fatalf("mapped preview = %+v", preview.Rows)
	}
	if _, err := svc.CommitImport(ctx, custom, service.ImportOptions{Mapping: mapping}); err != nil {
		t.Fatal(err)
	}
	list, _ := rd.ListContracts(ctx, model.ListFilter{Year: 2025})
	if list.Total != 1 || list.Items[0].Amount != 99.9 || list.Items[0].Data["area_sqm"] != 12.0 {
		t.Fatalf("mapped contract = %+v", list.Items)
	}

	// create mode does not overwrite an existing contract number.
	workbook = editWorkbook(t, ledger, func(f *excelize.File, header []string) {
		setCell(t, f, header, "金额（元）", 2, "999")
	})
	preview, err = svc.PreviewImport(ctx, workbook, service.ImportOptions{Mode: "create"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary.Error != 1 {
		t.Fatalf("create mode preview = %+v", preview.Summary)
	}
}

func TestImportRejectsUnknownFieldColumn(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	workbook := newWorkbook(t, []string{"合同名称", "不存在字段"}, [][]string{{"合同", "x"}})
	preview, err := svc.PreviewImport(ctx, workbook, service.ImportOptions{
		Mapping: map[string]string{"1": "nope"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Columns[1].Field != "" {
		t.Fatalf("unknown field should not map: %+v", preview.Columns[1])
	}
}

/* ---------- Excel 测试辅助 ---------- */

func readHeader(t *testing.T, data []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("合同台账")
	if err != nil || len(rows) == 0 {
		t.Fatalf("read header: %v", err)
	}
	return rows[0]
}

func readCell(t *testing.T, data []byte, header []string, row int, label string) string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cell, err := excelize.CoordinatesToCellName(columnIndex(header, label)+1, row)
	if err != nil {
		t.Fatal(err)
	}
	value, err := f.GetCellValue("合同台账", cell)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func columnIndex(header []string, label string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == label {
			return i
		}
	}
	return -1
}

func setCell(t *testing.T, f *excelize.File, header []string, label string, row int, value string) {
	t.Helper()
	idx := columnIndex(header, label)
	if idx < 0 {
		t.Fatalf("column %q not found in %v", label, header)
	}
	cell, err := excelize.CoordinatesToCellName(idx+1, row)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("合同台账", cell, value); err != nil {
		t.Fatal(err)
	}
}

func editWorkbook(t *testing.T, data []byte, mutate func(*excelize.File, []string)) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("合同台账")
	if err != nil || len(rows) == 0 {
		t.Fatalf("read workbook: %v", err)
	}
	mutate(f, rows[0])
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newWorkbook(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sheet := "合同台账"
	if _, err := f.NewSheet(sheet); err != nil {
		t.Fatal(err)
	}
	_ = f.DeleteSheet("Sheet1")
	for i, h := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for r, row := range rows {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSignatureRetentionLinksContractAndContent(t *testing.T) {
	svc, rd, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()

	party := mustParty(t, svc, "张三")
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "房屋租赁合同", PartyBID: party.ID, Amount: 1200,
		Data: map[string]any{},
	})
	digest, err := service.ContentSHA256(contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 {
		t.Fatalf("content digest = %q", digest)
	}
	// Changing any content field changes the version digest.
	changed := contract
	changed.Title = "房屋租赁合同（修订）"
	changedDigest, err := service.ContentSHA256(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedDigest == digest {
		t.Fatal("content digest ignored a title change")
	}

	img := image.NewNRGBA(image.Rect(0, 0, 600, 200))
	for y := 60; y < 90; y++ {
		for x := 80; x < 520; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	sig, err := svc.SaveSignature(ctx, service.SignatureInput{
		PartyID:       party.ID,
		ContractID:    contract.ID,
		SignerName:    party.Name,
		ContentSHA256: digest,
		Source:        model.SignatureSourceLANQR,
		SessionID:     "ssn_test",
		ImagePNG:      buf.Bytes(),
		SHA256:        hex.EncodeToString(sum[:]),
		WidthPx:       600,
		HeightPx:      200,
	})
	if err != nil {
		t.Fatalf("save signature: %v", err)
	}
	if sig.ContractID != contract.ID || sig.ContentSHA256 != digest ||
		sig.Source != model.SignatureSourceLANQR || sig.SessionID != "ssn_test" {
		t.Fatalf("signature links = %+v", sig)
	}
	stored, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(sig.AssetPath)))
	if err != nil {
		t.Fatalf("signature file missing: %v", err)
	}
	if !bytes.Equal(stored, buf.Bytes()) {
		t.Fatal("stored signature differs from submitted bytes")
	}

	// The signature is queryable by contract and appears in contract detail.
	list, err := rd.ListSignatures(ctx, model.ListFilter{ContractID: contract.ID})
	if err != nil || list.Total != 1 || list.Items[0].ID != sig.ID {
		t.Fatalf("list by contract = %+v err=%v", list, err)
	}
	detail, err := rd.GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Signatures) != 1 || detail.Signatures[0].ContentSHA256 != digest {
		t.Fatalf("contract detail signatures = %+v", detail.Signatures)
	}

	// The audit log records the capture against the contract.
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: "contract", EntityID: contract.ID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range logs.Items {
		if strings.Contains(entry.Summary, "手写签名") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing signature change-log entry: %+v", logs.Items)
	}

	// A missing contract is rejected before any file is written.
	if _, err := svc.SaveSignature(ctx, service.SignatureInput{
		ContractID: "ctr_missing", SignerName: "张三",
		ImagePNG: buf.Bytes(), SHA256: hex.EncodeToString(sum[:]),
	}); err == nil {
		t.Fatal("missing contract should be rejected")
	}
	// A digest that does not match the bytes is rejected too.
	if _, err := svc.SaveSignature(ctx, service.SignatureInput{
		ContractID: contract.ID, SignerName: "张三",
		ImagePNG: buf.Bytes(), SHA256: strings.Repeat("0", 64),
	}); err == nil {
		t.Fatal("mismatched digest should be rejected")
	}
}
