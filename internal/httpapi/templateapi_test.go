package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
)

func testTemplatePDF(t *testing.T) []byte {
	t.Helper()
	family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{})
	if err != nil {
		t.Skipf("no CJK font available: %v", err)
	}
	var buf bytes.Buffer
	const wMM, hMM = 210.0, 297.0
	r := pdf.New(&buf, wMM, hMM, &pdf.Options{Compress: true, SubsetFonts: true})
	c := canvas.New(wMM, hMM)
	ctx := canvas.NewContext(c)
	face := family.Face(18, color.RGBA{A: 255}, canvas.FontRegular)
	ctx.DrawText(25, 270, canvas.NewTextLine(face, "房屋租赁合同模板", canvas.Left))
	ctx.DrawText(25, 250, canvas.NewTextLine(face, "合同编号：", canvas.Left))
	c.RenderTo(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTemplateAPIImportCalibratePreviewGenerate(t *testing.T) {
	f := newFixture(t)

	// Converter availability is reported so the UI can show install hints.
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/converters", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("converters status = %d body=%s", resp.StatusCode, body)
	}
	var converters struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &converters); err != nil {
		t.Fatal(err)
	}
	if len(converters.Items) != 2 {
		t.Fatalf("converters = %s", body)
	}

	pdf := testTemplatePDF(t)
	importBody := map[string]any{
		"name":        "房屋租赁模板",
		"file_name":   "test-template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import", importBody, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	templateID, _ := item["id"].(string)
	if templateID == "" || item["kind"] != "pdf" {
		t.Fatalf("imported item = %s", body)
	}
	if version, _ := item["version"].(float64); int(version) != 1 {
		t.Fatalf("version = %v", item["version"])
	}

	// Listing filters by kind and includes the new template.
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/templates?kind=pdf", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), templateID) {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}

	// The converted template PDF is served for the PDF.js editor.
	resp, body = f.get(t, "/app/api/v1/templates/"+templateID+"/pdf", false)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("%PDF")) {
		t.Fatalf("template pdf status=%d prefix=%q", resp.StatusCode, body[:min(10, len(body))])
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("content-type = %q", ct)
	}

	// Unknown bindings are rejected with a field-attributed error.
	bad := map[string]any{"placements": []map[string]any{{
		"id": "bad", "page": 1, "x": 100, "y": 100, "w": 200, "h": 20,
		"type": "field", "field": "contract.not_a_field",
	}}}
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/templates/"+templateID+"/mapping", bad, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "未知的合同字段") {
		t.Fatalf("bad mapping status=%d body=%s", resp.StatusCode, body)
	}

	placements := []map[string]any{
		{"id": "no", "page": 1, "x": 110, "y": 150, "w": 220, "h": 22,
			"type": "field", "field": "contract.contract_no"},
		{"id": "title", "page": 1, "x": 110, "y": 190, "w": 260, "h": 22,
			"type": "field", "field": "contract.title"},
	}
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/templates/"+templateID+"/mapping",
		map[string]any{"placements": placements}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save mapping status=%d body=%s", resp.StatusCode, body)
	}
	item = decodeAppItem(t, body)
	if version, _ := item["version"].(float64); int(version) != 2 {
		t.Fatalf("saved version = %v", item["version"])
	}

	// Sample preview returns a PDF and a digest for generate.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/preview",
		map[string]any{"sample": true}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sample preview status=%d body=%s", resp.StatusCode, body)
	}
	var samplePreview struct {
		PDFBase64     string `json:"pdf_base64"`
		PreviewSHA256 string `json:"preview_sha256"`
		PageCount     int    `json:"page_count"`
	}
	if err := json.Unmarshal(body, &samplePreview); err != nil {
		t.Fatal(err)
	}
	if samplePreview.PDFBase64 == "" || len(samplePreview.PreviewSHA256) != 64 || samplePreview.PageCount != 1 {
		t.Fatalf("sample preview = %s", body)
	}

	// Real preview over the fixture contract.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/preview",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("contract preview status=%d body=%s", resp.StatusCode, body)
	}
	var preview struct {
		PDFBase64     string `json:"pdf_base64"`
		PreviewSHA256 string `json:"preview_sha256"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PDFBase64 == "" || len(preview.PreviewSHA256) != 64 {
		t.Fatalf("preview = %s", body)
	}

	// Generate without the preview digest is a conflict.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "preview_required") {
		t.Fatalf("generate without preview status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{"contract_id": f.contractID, "preview_sha256": preview.PreviewSHA256}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", resp.StatusCode, body)
	}
	doc := decodeAppItem(t, body)
	filePath, _ := doc["file_path"].(string)
	if filePath == "" {
		t.Fatalf("document = %s", body)
	}
	full := filepath.Join(f.dataDir, filepath.FromSlash(filePath))
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("output file: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatal("output is not a PDF")
	}
	docID, _ := doc["id"].(string)
	resp, body = f.get(t, "/app/api/v1/documents/"+docID+"/file", false)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("%PDF")) {
		t.Fatalf("document file status=%d prefix=%q", resp.StatusCode, body[:min(10, len(body))])
	}

	// Usage reports the generated document and the pinned contract.
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/templates/"+templateID+"/usage", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", resp.StatusCode, body)
	}
	var usage map[string]any
	if err := json.Unmarshal(body, &usage); err != nil {
		t.Fatal(err)
	}
	if usage["documents"].(float64) != 1 || usage["version_contracts"].(float64) != 1 {
		t.Fatalf("usage = %s", body)
	}
}

// te-rccn0：保存标定时返回结构化重叠提醒（不阻断），并持久化白底遮盖开关。
func TestTemplateAPISaveMappingOverlapsAndMask(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import", map[string]any{
		"name":        "重叠提醒模板",
		"file_name":   "overlap-template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(testTemplatePDF(t)),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, body)
	}
	templateID, _ := decodeAppItem(t, body)["id"].(string)
	if templateID == "" {
		t.Fatalf("imported template id missing: %s", body)
	}

	placements := []map[string]any{
		{"id": "field", "page": 1, "x": 100, "y": 100, "w": 100, "h": 20,
			"type": "field", "field": "contract.contract_no"},
		{"id": "seal", "page": 1, "x": 120, "y": 100, "w": 100, "h": 100,
			"type": "seal", "seal_mode": "contract"},
	}
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/templates/"+templateID+"/mapping",
		map[string]any{"placements": placements, "mask_field_values": true}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save mapping status=%d body=%s", resp.StatusCode, body)
	}
	var saved struct {
		Item struct {
			ID              string `json:"id"`
			MaskFieldValues bool   `json:"mask_field_values"`
		} `json:"item"`
		Warnings []string `json:"warnings"`
		Overlaps []struct {
			A     string  `json:"a"`
			B     string  `json:"b"`
			Ratio float64 `json:"ratio"`
		} `json:"overlaps"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Item.MaskFieldValues {
		t.Fatalf("mask flag not saved: %s", body)
	}
	if len(saved.Overlaps) != 1 || saved.Overlaps[0].A != "seal" || saved.Overlaps[0].B != "field" {
		t.Fatalf("overlaps = %s", body)
	}
	if saved.Overlaps[0].Ratio != 0.8 {
		t.Fatalf("overlap ratio = %v, want 0.8", saved.Overlaps[0].Ratio)
	}
	overlapWarned := false
	for _, warning := range saved.Warnings {
		if strings.Contains(warning, "重叠") {
			overlapWarned = true
		}
	}
	if !overlapWarned {
		t.Fatalf("warnings = %s", body)
	}

	// The saved version keeps the flag when read back.
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/templates/"+saved.Item.ID, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get template status=%d body=%s", resp.StatusCode, body)
	}
	var got struct {
		MaskFieldValues bool `json:"mask_field_values"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.MaskFieldValues {
		t.Fatalf("mask flag lost on read: %s", body)
	}
}

func TestTemplateAPIImportRejectsInvalidFile(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import",
		map[string]any{"name": "坏文件", "file_name": "x.docx",
			"file_base64": base64.StdEncoding.EncodeToString([]byte("not a zip"))}, nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_file") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import",
		map[string]any{"name": "空文件"}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty status=%d body=%s", resp.StatusCode, body)
	}
}

// deadlineWriter 记录响应写截止时间的变化，用来证明导入处理器在长耗时转换
// 前清除了服务端 WriteTimeout（否则超过 30s 的响应永远写不回浏览器）。
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestTemplateImportClearsWriteDeadline(t *testing.T) {
	f := newFixture(t)
	pdf := testTemplatePDF(t)
	payload, err := json.Marshal(map[string]any{
		"name":        "写截止时间模板",
		"file_name":   "deadline-template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/app/api/v1/templates/import", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345" // 本机请求才能通过 loopback 守卫
	req.Header.Set("X-Tengshou-App", "1")
	req.Header.Set("Content-Type", "application/json")
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	f.handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", w.Code, w.Body.String())
	}
	if len(w.deadlines) == 0 {
		t.Fatal("导入处理器没有清除写截止时间")
	}
	if last := w.deadlines[len(w.deadlines)-1]; !last.IsZero() {
		t.Fatalf("写截止时间 = %v，want 零值（无截止）", last)
	}
}

func TestPDFJSAssetsAreServed(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/app/vendor/pdf.min.js", false)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "pdfjs") {
		t.Fatalf("pdf.js status=%d len=%d", resp.StatusCode, len(body))
	}
	resp, body = f.get(t, "/app/vendor/pdf.worker.min.js", false)
	if resp.StatusCode != http.StatusOK || len(body) < 100000 {
		t.Fatalf("pdf.worker status=%d len=%d", resp.StatusCode, len(body))
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "worker-src 'self'") {
		t.Fatalf("worker CSP = %q", csp)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
