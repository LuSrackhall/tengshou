package mcp

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// importPDFTemplate imports a blank one-page PDF and saves the placements as a
// new version, mirroring the desktop calibration flow. The PDF carries no text
// so template fixtures never depend on a CJK font.
func (env *testEnv) importPDFTemplate(t *testing.T, name string, placements []model.Placement) model.Template {
	t.Helper()
	var buf bytes.Buffer
	const wMM, hMM = 210.0, 297.0
	renderer := pdf.New(&buf, wMM, hMM, &pdf.Options{Compress: true})
	doc := canvas.New(wMM, hMM)
	ctx := canvas.NewContext(doc)
	ctx.SetFillColor(color.RGBA{A: 255})
	ctx.DrawPath(25, 250, canvas.Rectangle(80, 4))
	doc.RenderTo(renderer)
	if err := renderer.Close(); err != nil {
		t.Fatalf("render template pdf: %v", err)
	}
	imported, err := env.svc.ImportTemplate(context.Background(), service.TemplateImportInput{
		Name: name, FileName: name + ".pdf", Data: buf.Bytes(),
	})
	if err != nil {
		t.Fatalf("import template: %v", err)
	}
	saved, err := env.svc.SaveTemplateMapping(context.Background(), imported.Template.ID,
		service.TemplateSaveInput{Placements: placements})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	return saved.Template
}

// requireGenerationFont pins the overlay font or skips the test on machines
// without a CJK font (the same guard the service tests use).
func requireGenerationFont(t *testing.T, svc *service.Service) {
	t.Helper()
	family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{})
	if err != nil {
		t.Skipf("no CJK font available for PDF generation: %v", err)
	}
	svc.SetFontLoader(func() (*canvas.FontFamily, error) { return family, nil })
}

func TestPreflightAndGenerateContractTool(t *testing.T) {
	env := newTestEnv(t, true)
	requireGenerationFont(t, env.svc)
	srv := serverFor(t, env)
	ctx := context.Background()
	tpl := env.importPDFTemplate(t, "无签名位模板", nil)

	preflight := callToolJSON(t, srv, toolPreflightGeneration, map[string]any{
		"template_id": tpl.ID, "contract_id": env.contractID,
	})
	if preflight["ready"] != true {
		t.Fatalf("preflight not ready: %v", preflight)
	}
	if sha, _ := preflight["preview_sha256"].(string); len(sha) != 64 {
		t.Fatalf("preview_sha256 = %q", preflight["preview_sha256"])
	}
	if preflight["previous_documents"] != float64(0) {
		t.Fatalf("previous_documents = %v", preflight["previous_documents"])
	}

	generated := callToolJSON(t, srv, toolGenerateContract, map[string]any{
		"template_id": tpl.ID, "contract_id": env.contractID,
	})
	item, _ := generated["item"].(map[string]any)
	if item["status"] != model.DocumentStatusSuccess {
		t.Fatalf("generated item = %v", item)
	}
	if item["operator"] != service.MCPActor {
		t.Fatalf("document operator = %v, want %q", item["operator"], service.MCPActor)
	}
	if item["template_version"] != float64(tpl.Version) {
		t.Fatalf("template_version = %v, want %d", item["template_version"], tpl.Version)
	}
	if item["kind"] != model.DocumentKindInitial {
		t.Fatalf("kind = %v", item["kind"])
	}
	if _, ok := generated["warnings"]; !ok {
		t.Fatalf("generate result missing warnings: %v", generated)
	}
	docID, _ := item["id"].(string)
	if docID == "" {
		t.Fatalf("document id missing: %v", item)
	}

	docs, err := env.reader.ListDocuments(ctx, model.ListFilter{ContractID: env.contractID})
	if err != nil {
		t.Fatal(err)
	}
	if docs.Total != 1 || docs.Items[0].Status != model.DocumentStatusSuccess {
		t.Fatalf("persisted documents = %+v", docs)
	}

	// The generated PDF is downloadable through the path the tool reports.
	resp, err := http.Get(env.server.URL + "/app/api/v1/documents/" + docID + "/file")
	if err != nil {
		t.Fatalf("download document: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatalf("download status = %d bytes=%d", resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Fatalf("download content-type = %q", ct)
	}

	listed := callToolJSON(t, srv, toolListDocuments, map[string]any{
		"contract_id": env.contractID,
	})
	if listed["total"] != float64(1) {
		t.Fatalf("documents list = %v", listed)
	}
	row, _ := listed["items"].([]any)[0].(map[string]any)
	wantPath := "/app/api/v1/documents/" + docID + "/file"
	if row["id"] != docID || row["download_path"] != wantPath {
		t.Fatalf("document row = %v", row)
	}
	if row["contract_no"] != "HT-2026-0001" {
		t.Fatalf("contract_no = %v", row["contract_no"])
	}
	if row["template_name"] != tpl.Name || row["template_version"] != float64(tpl.Version) {
		t.Fatalf("template projection = %v", row)
	}
	if row["status"] != model.DocumentStatusSuccess || row["kind"] != model.DocumentKindInitial {
		t.Fatalf("document state = %v", row)
	}

	// Generation is a write: the change log must attribute it to the agent.
	assertMCPActor(t, env.reader, "contract", env.contractID)
}

func TestGenerateContractToolBlocksUnsignedTemplate(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)
	ctx := context.Background()

	tpl := env.importPDFTemplate(t, "签名位模板", []model.Placement{{
		ID: "sig", Page: 1, X: 300, Y: 600, W: 140, H: 60,
		Type: model.PlacementSignature, Party: model.PlacementPartyB,
	}})
	unsigned := &model.Contract{
		Year: 2026, Title: "未签名合同", ContractNo: "HT-2026-0099",
		PartyBID: env.partyBID, Status: model.ContractActive, Data: map[string]any{},
	}
	if err := env.store.PutContract(ctx, unsigned); err != nil {
		t.Fatal(err)
	}

	preflight := callToolJSON(t, srv, toolPreflightGeneration, map[string]any{
		"template_id": tpl.ID, "contract_id": unsigned.ID,
	})
	if preflight["ready"] != false {
		t.Fatalf("preflight should be blocked: %v", preflight)
	}
	issues, _ := preflight["issues"].([]any)
	if !hasIssueCode(issues, "missing_signature") {
		t.Fatalf("preflight issues = %v", issues)
	}

	text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"template_id": tpl.ID, "contract_id": unsigned.ID,
	})
	if !isError || !strings.Contains(text, "missing_signature") {
		t.Fatalf("blocked generate: isError=%v text=%s", isError, text)
	}
	docs, err := env.reader.ListDocuments(ctx, model.ListFilter{ContractID: unsigned.ID})
	if err != nil {
		t.Fatal(err)
	}
	if docs.Total != 0 {
		t.Fatalf("blocked generate must not record documents: %+v", docs.Items)
	}
}

func hasIssueCode(issues []any, code string) bool {
	for _, raw := range issues {
		issue, _ := raw.(map[string]any)
		if issue["code"] == code {
			return true
		}
	}
	return false
}

func TestListDocumentsFiltersAndPagination(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	rows := []*model.OutputDocument{
		{ContractID: env.contractID, TemplateID: env.templateID, TemplateVersion: 1,
			Kind: model.DocumentKindInitial, Status: model.DocumentStatusSuccess,
			FilePath: "outputs/a.pdf", SHA256: "sha-a", CreatedAt: base},
		{ContractID: env.contractID, TemplateID: env.templateID, TemplateVersion: 1,
			Kind: model.DocumentKindRegenerate, Status: model.DocumentStatusSuccess,
			FilePath: "outputs/b.pdf", SHA256: "sha-b", CreatedAt: base.Add(time.Second)},
		{ContractID: env.contractID, TemplateID: env.templateID, TemplateVersion: 1,
			Kind: model.DocumentKindInitial, Status: model.DocumentStatusFailed,
			ErrorMessage: "缺少签名", CreatedAt: base.Add(2 * time.Second)},
	}
	for _, row := range rows {
		if err := env.store.PutOutputDocument(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	all := callToolJSON(t, srv, toolListDocuments, map[string]any{})
	if all["total"] != float64(3) || all["limit"] != float64(50) {
		t.Fatalf("all documents = %v", all)
	}

	success := callToolJSON(t, srv, toolListDocuments, map[string]any{"status": "success"})
	if success["total"] != float64(2) {
		t.Fatalf("success documents = %v", success)
	}
	failed := callToolJSON(t, srv, toolListDocuments, map[string]any{"status": "failed"})
	if failed["total"] != float64(1) {
		t.Fatalf("failed documents = %v", failed)
	}
	failedRow, _ := failed["items"].([]any)[0].(map[string]any)
	if failedRow["error_message"] != "缺少签名" || failedRow["status"] != model.DocumentStatusFailed {
		t.Fatalf("failed row = %v", failedRow)
	}

	page1 := callToolJSON(t, srv, toolListDocuments, map[string]any{"limit": 1, "offset": 0})
	page2 := callToolJSON(t, srv, toolListDocuments, map[string]any{"limit": 1, "offset": 2})
	if page1["total"] != float64(3) || page2["total"] != float64(3) {
		t.Fatalf("pagination totals = %v / %v", page1, page2)
	}
	items1, _ := page1["items"].([]any)
	items2, _ := page2["items"].([]any)
	if len(items1) != 1 || len(items2) != 1 {
		t.Fatalf("page sizes = %d / %d", len(items1), len(items2))
	}
	id1 := items1[0].(map[string]any)["id"]
	id2 := items2[0].(map[string]any)["id"]
	if id1 == id2 {
		t.Fatalf("pagination returned the same row %v on both pages", id1)
	}

	for _, args := range []map[string]any{
		{"status": "archived"},
		{"limit": 1000},
		{"offset": -1},
	} {
		if text, isError := callTool(t, srv, toolListDocuments, args); !isError || text == "" {
			t.Fatalf("invalid list args %v: isError=%v text=%s", args, isError, text)
		}
	}
}

func TestGenerateContractToolRespectsWriteSwitch(t *testing.T) {
	env := newTestEnv(t, false)
	srv := serverFor(t, env)
	ctx := context.Background()
	tpl := env.importPDFTemplate(t, "写开关模板", nil)

	// Reads are unaffected: preflight and document queries still work.
	preflight := callToolJSON(t, srv, toolPreflightGeneration, map[string]any{
		"template_id": tpl.ID, "contract_id": env.contractID,
	})
	if preflight["ready"] != true {
		t.Fatalf("preflight with writes off = %v", preflight)
	}
	callToolJSON(t, srv, toolListDocuments, map[string]any{})

	text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"template_id": tpl.ID, "contract_id": env.contractID,
	})
	if !isError || !strings.Contains(text, "mcp_write_disabled") {
		t.Fatalf("generate with writes off: isError=%v text=%s", isError, text)
	}
	docs, err := env.reader.ListDocuments(ctx, model.ListFilter{ContractID: env.contractID})
	if err != nil {
		t.Fatal(err)
	}
	if docs.Total != 0 {
		t.Fatalf("rejected generate must not record documents: %+v", docs.Items)
	}
}

func TestGenerationToolArgumentValidation(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)

	if text, isError := callTool(t, srv, toolPreflightGeneration, map[string]any{
		"template_id": env.templateID,
	}); !isError || !strings.Contains(text, "contract_id") {
		t.Fatalf("preflight without contract: isError=%v text=%s", isError, text)
	}
	if text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"contract_id": env.contractID,
	}); !isError || !strings.Contains(text, "template_id") {
		t.Fatalf("generate without template: isError=%v text=%s", isError, text)
	}
	if text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"template_id": env.templateID, "contract_id": env.contractID, "kind": "bogus",
	}); !isError || !strings.Contains(text, "kind") {
		t.Fatalf("generate with bad kind: isError=%v text=%s", isError, text)
	}
	if text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"template_id": env.templateID, "contract_id": env.contractID, "preview_sha256": "forged",
	}); !isError || !strings.Contains(text, "参数错误") {
		t.Fatalf("unknown generate arg: isError=%v text=%s", isError, text)
	}
	if text, isError := callTool(t, srv, toolGenerateContract, map[string]any{
		"template_id": env.templateID, "contract_id": env.contractID,
	}); !isError || !strings.Contains(text, "invalid_kind") {
		// The fixture template is HTML; the server rejects non-PDF templates.
		t.Fatalf("generate with html template: isError=%v text=%s", isError, text)
	}
}
