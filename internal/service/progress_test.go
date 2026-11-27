package service_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// recordingProgress collects every update so tests can pin stages, messages
// and the real i/N counters without a UI in the loop.
func recordingProgress() (service.ProgressFunc, *[]service.Progress) {
	updates := []service.Progress{}
	return func(p service.Progress) { updates = append(updates, p) }, &updates
}

func TestImportTemplateReportsProgress(t *testing.T) {
	f := newTemplateFixture(t)
	report, updates := recordingProgress()
	res, err := f.svc.ImportTemplate(context.Background(), service.TemplateImportInput{
		Name: "进度模板", FileName: "progress.pdf", Data: f.templatePDF(t),
		Progress: report,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Template.ID == "" {
		t.Fatal("import returned no template")
	}
	if len(*updates) < 3 {
		t.Fatalf("progress updates = %+v", *updates)
	}
	if (*updates)[0].Stage != service.ProgressSaving ||
		!strings.Contains((*updates)[0].Message, "正在保存文件") {
		t.Fatalf("first update = %+v", (*updates)[0])
	}
	var stages []string
	for _, update := range *updates {
		stages = append(stages, update.Stage)
		if update.Stage == service.ProgressConverting {
			t.Fatalf("PDF import must not report a conversion stage: %+v", *updates)
		}
	}
	wantPrefix := []string{service.ProgressSaving, service.ProgressParsing, service.ProgressSaving}
	if strings.Join(stages[:3], ",") != strings.Join(wantPrefix, ",") {
		t.Fatalf("stages = %v, want prefix %v", stages, wantPrefix)
	}
	if !strings.Contains((*updates)[1].Message, "正在解析 PDF") {
		t.Fatalf("parsing message = %q", (*updates)[1].Message)
	}
}

func TestBatchGenerateReportsRealItemProgress(t *testing.T) {
	f := newTemplateFixture(t)
	imported := f.importTemplate(t, "批量进度模板")
	report, updates := recordingProgress()
	res, err := f.svc.GenerateContractBatch(context.Background(), service.BatchGenerateInput{
		TemplateID: imported.Template.ID,
		// One empty id and one missing contract: both fail fast without any
		// rendering, but the i/N progress must still be real and monotonic.
		Items: []service.BatchGenerateItemInput{
			{ContractID: ""},
			{ContractID: "ctr_missing"},
		},
		Progress: report,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Total != 2 || res.Summary.Failed != 2 {
		t.Fatalf("summary = %+v", res.Summary)
	}
	if len(*updates) == 0 {
		t.Fatal("no batch progress reported")
	}
	sawSecond := false
	for _, update := range *updates {
		if update.Stage != service.ProgressBatch {
			t.Fatalf("batch stage = %q", update.Stage)
		}
		if update.Total != 2 || update.Current < 0 || update.Current > 2 {
			t.Fatalf("batch counters = %d/%d", update.Current, update.Total)
		}
		if strings.Contains(update.Message, "正在生成第 2/2 份") {
			sawSecond = true
		}
	}
	if !sawSecond {
		t.Fatalf("missing 2/2 generation message: %+v", *updates)
	}
	last := (*updates)[len(*updates)-1]
	if last.Current != 2 || last.Total != 2 {
		t.Fatalf("final progress = %d/%d", last.Current, last.Total)
	}
}

func TestGenerateContractDocumentReportsProgress(t *testing.T) {
	f := newTemplateFixture(t)
	imported := f.importTemplate(t, "单份进度模板")
	party := mustParty(t, f.svc, "进度测试租户")
	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "进度合同", PartyBID: party.ID,
	})
	report, updates := recordingProgress()
	_, err := f.svc.GenerateContractDocument(context.Background(), service.GenerateInput{
		TemplateID: imported.Template.ID, ContractID: contract.ID,
		PreviewSHA256: "not-the-preview-digest", Progress: report,
	})
	mustErrorCode(t, err, "preview_required")
	if len(*updates) == 0 {
		t.Fatal("no progress reported before the digest gate")
	}
	first := (*updates)[0]
	if first.Stage != service.ProgressRendering || !strings.Contains(first.Message, "正在准备数据") {
		t.Fatalf("first update = %+v", first)
	}
}

func TestExcelPreviewReportsParseAndValidateStages(t *testing.T) {
	f := newTemplateFixture(t)
	data, err := f.svc.TemplateWorkbook(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	report, updates := recordingProgress()
	if _, err := f.svc.PreviewImport(context.Background(), data, service.ImportOptions{Progress: report}); err != nil {
		t.Fatal(err)
	}
	if len(*updates) == 0 {
		t.Fatal("no excel progress reported")
	}
	if !strings.Contains((*updates)[0].Message, "正在解析表格") {
		t.Fatalf("first update = %+v", (*updates)[0])
	}
	sawValidate := false
	for _, update := range *updates {
		if strings.Contains(update.Message, "正在校验") {
			sawValidate = true
		}
	}
	if !sawValidate {
		t.Fatalf("missing validation stage: %+v", *updates)
	}
}

// Excel 提交必须逐行上报真实写入进度（「正在写入第 i/N 行…」）：文案、
// current 单调递增与 total 都由服务端驱动，界面据此渲染确定进度条。
func TestExcelCommitReportsRowProgress(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	template, err := svc.TemplateWorkbook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workbook := editWorkbook(t, template, func(f *excelize.File, header []string) {
		for offset, name := range []string{"进度行甲", "进度行乙"} {
			row := offset + 2
			setCell(t, f, header, "年度", row, "2026")
			setCell(t, f, header, "合同名称", row, name)
			setCell(t, f, header, "相对方", row, name+"租户")
			setCell(t, f, header, "金额（元）", row, "1000")
		}
	})
	report, updates := recordingProgress()
	res, err := svc.CommitImport(ctx, workbook, service.ImportOptions{Progress: report})
	if err != nil {
		t.Fatalf("commit import: %v", err)
	}
	if res.Created != 2 {
		t.Fatalf("commit result = %+v", res)
	}
	var writes []service.Progress
	for _, update := range *updates {
		if update.Stage == service.ProgressWriting {
			writes = append(writes, update)
		}
	}
	if len(writes) != 2 {
		t.Fatalf("writing updates = %+v", writes)
	}
	for i, update := range writes {
		want := fmt.Sprintf("正在写入第 %d/2 行…", i+1)
		if update.Current != i+1 || update.Total != 2 || update.Message != want {
			t.Fatalf("writing update %d = %+v, want %q", i, update, want)
		}
	}
}

// 成功生成的完整阶段序列：准备数据 → 套印并生成 PDF → 保存生成记录。
// 失败路径（digest 门禁）只覆盖首阶段，这里把成功路径的三段文案也 pin 住。
func TestGenerateContractDocumentReportsProgressOnSuccessPath(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	imported := f.importTemplate(t, "成功进度模板")
	party := mustParty(t, f.svc, "成功进度租户")
	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "成功进度合同", PartyBID: party.ID,
	})
	// 一个纯字段位置即可通过 preflight；本用例只关心阶段文案。
	saved, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "no", Page: 1, X: 120, Y: 150, W: 200, H: 22,
				Type: model.PlacementField, Field: "contract.contract_no"},
		},
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	preview, err := f.svc.PreviewTemplate(ctx, saved.Template.ID, service.TemplatePreviewInput{
		ContractID: contract.ID,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	report, updates := recordingProgress()
	if _, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: saved.Template.ID, ContractID: contract.ID,
		PreviewSHA256: preview.PreviewSHA256, Progress: report,
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	renderSeq := []string{"正在准备数据", "正在套印并生成 PDF"}
	var stages []string
	sawWrite := false
	for _, update := range *updates {
		stages = append(stages, update.Stage+":"+update.Message)
		if update.Stage == service.ProgressRendering && len(renderSeq) > 0 &&
			strings.Contains(update.Message, renderSeq[0]) {
			renderSeq = renderSeq[1:]
		}
		if update.Stage == service.ProgressWriting && strings.Contains(update.Message, "正在保存生成记录") {
			sawWrite = true
		}
	}
	if len(renderSeq) != 0 {
		t.Fatalf("missing rendering stage(s) %v in %v", renderSeq, stages)
	}
	if !sawWrite {
		t.Fatalf("missing writing stage in %v", stages)
	}
}
