package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// 金额大写覆盖（te-7c5v1）：保留键随合同保存、部分更新保留、空值清除，
// 非金额字段的覆盖被拒绝；续签草稿原样带出覆盖值。
func TestContractMoneyWordsOverrideLifecycle(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	for _, field := range []service.FieldInput{
		{
			Entity: model.FieldEntityContract, Key: "rent", Label: "月租",
			Type: model.FieldTypeMoney, InLedger: boolPtr(true), InTemplate: boolPtr(true),
		},
		{
			Entity: model.FieldEntityContract, Key: "note", Label: "备注",
			Type: model.FieldTypeText, InLedger: boolPtr(true), InTemplate: boolPtr(true),
		},
	} {
		if _, err := svc.CreateField(ctx, field); err != nil {
			t.Fatal(err)
		}
	}
	base := service.ContractInput{
		Year: 2026, Title: "金额大写合同", PartyBID: party.ID, Amount: 1500.5,
		Data: map[string]any{
			"rent":             1000.05,
			"__amount_words__": "人民币手填金额",
			"__words__rent":    "人民币手填月租",
		},
	}
	contract := mustContract(t, svc, base)
	if contract.Data["__amount_words__"] != "人民币手填金额" ||
		contract.Data["__words__rent"] != "人民币手填月租" {
		t.Fatalf("override not stored: %#v", contract.Data)
	}

	// 表单之外的部分更新（例如 MCP 省略保留键）必须保留已有覆盖。
	update := base
	update.ID = contract.ID
	update.Data = map[string]any{"rent": 1000.05}
	updated := mustContract(t, svc, update)
	if updated.Data["__amount_words__"] != "人民币手填金额" ||
		updated.Data["__words__rent"] != "人民币手填月租" {
		t.Fatalf("partial update lost override: %#v", updated.Data)
	}

	// 空串是显式清除：恢复自动大写。
	update.Data = map[string]any{"rent": 1000.05, "__amount_words__": "", "__words__rent": "  "}
	cleared := mustContract(t, svc, update)
	if _, ok := cleared.Data["__amount_words__"]; ok {
		t.Fatalf("amount override not cleared: %#v", cleared.Data)
	}
	if _, ok := cleared.Data["__words__rent"]; ok {
		t.Fatalf("dynamic override not cleared: %#v", cleared.Data)
	}

	// 非金额字段、不存在的字段与未定义数据键都不接受覆盖。
	for _, data := range []map[string]any{
		{"__words__note": "人民币备注"},
		{"__words__missing": "x"},
		{"__amount_words__": "x", "undefined": "x"},
	} {
		update.Data = data
		if _, _, err := svc.SaveContract(ctx, update); err == nil {
			t.Fatalf("override %v should be rejected", data)
		}
	}
}

// 续签草稿与续签保存都要带出金额大写覆盖；覆盖键不算「已带出字段」。
func TestRenewalKeepsMoneyWordsOverride(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "续签租户")
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "rent", Label: "月租",
		Type: model.FieldTypeMoney, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	source := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "续签来源合同", PartyBID: party.ID, Amount: 1200,
		Data: map[string]any{
			"rent":             1200,
			"__amount_words__": "人民币壹仟贰佰元整（手改）",
			"__words__rent":    "人民币月租手改",
		},
	})
	draft, err := svc.RenewalDraft(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Input.Data["__amount_words__"] != "人民币壹仟贰佰元整（手改）" ||
		draft.Input.Data["__words__rent"] != "人民币月租手改" {
		t.Fatalf("renewal draft lost override: %#v", draft.Input.Data)
	}
	if strings.Contains(strings.Join(draft.Reused, ","), "__") {
		t.Fatalf("reserved keys leaked into reused list: %v", draft.Reused)
	}
	renewed := mustContract(t, svc, draft.Input)
	reloaded, err := rd.GetContract(ctx, renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Data["__amount_words__"] != "人民币壹仟贰佰元整（手改）" ||
		reloaded.Data["__words__rent"] != "人民币月租手改" {
		t.Fatalf("renewed contract lost override: %#v", reloaded.Data)
	}
}

// Excel 导出不含保留键，往返导入也不改动已有覆盖（覆盖不是台账列）。
func TestExcelRoundTripIgnoresMoneyWordsOverrides(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "Excel 租户")
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "rent", Label: "月租",
		Type: model.FieldTypeMoney, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	contract := mustContract(t, svc, service.ContractInput{
		ContractNo: "WORDS-1", Year: 2026, Title: "Excel 大写合同", PartyBID: party.ID,
		Amount: 1500.5,
		Data: map[string]any{
			"rent":             1000.05,
			"__amount_words__": "人民币手填金额",
			"__words__rent":    "人民币手填月租",
		},
	})
	ledger, err := svc.LedgerWorkbook(ctx, model.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range readHeader(t, ledger) {
		if strings.Contains(header, "大写") || strings.Contains(header, "__") {
			t.Fatalf("ledger leaked reserved words column %q", header)
		}
	}

	// 改一个真实列触发更新；保留键不在文件里，导入后必须保持原值。
	workbook := editWorkbook(t, ledger, func(f *excelize.File, header []string) {
		setCell(t, f, header, "金额（元）", 2, "200")
	})
	preview, err := svc.PreviewImport(ctx, workbook, service.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary.Update != 1 || preview.Rows[0].Action != "update" {
		t.Fatalf("preview = %+v rows=%+v", preview.Summary, preview.Rows)
	}
	if _, err := svc.CommitImport(ctx, workbook, service.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	updated, err := rd.GetContract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Amount != 200 {
		t.Fatalf("amount not updated: %+v", updated)
	}
	if updated.Data["__amount_words__"] != "人民币手填金额" ||
		updated.Data["__words__rent"] != "人民币手填月租" {
		t.Fatalf("Excel import changed overrides: %#v", updated.Data)
	}
}
