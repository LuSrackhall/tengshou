package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pollJob polls GET /app/api/v1/jobs/{id} until the job leaves "running".
func pollJob(t *testing.T, f *fixture, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/jobs/"+jobID, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get job status = %d body=%s", resp.StatusCode, body)
		}
		var snap map[string]any
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("decode job: %v body=%s", err, body)
		}
		if snap["status"] != "running" {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still running after deadline; last=%s", jobID, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startJobRequest(t *testing.T, f *fixture, path string, body any) string {
	t.Helper()
	resp, raw := f.appRequest(t, http.MethodPost, path, body, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start job status = %d body=%s", resp.StatusCode, raw)
	}
	var start struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &start); err != nil {
		t.Fatalf("decode start: %v body=%s", err, raw)
	}
	if !strings.HasPrefix(start.JobID, "job_") {
		t.Fatalf("job id = %q", start.JobID)
	}
	return start.JobID
}

func TestJobTemplateImportRunsInBackground(t *testing.T) {
	f := newFixture(t)
	pdf := testTemplatePDF(t)
	jobID := startJobRequest(t, f, "/app/api/v1/jobs/templates/import", map[string]any{
		"name":        "后台导入模板",
		"file_name":   "background.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	})
	snap := pollJob(t, f, jobID)
	if snap["status"] != "done" || snap["kind"] != "template_import" {
		t.Fatalf("job snapshot = %+v", snap)
	}
	if elapsed, _ := snap["elapsed_ms"].(float64); elapsed < 0 {
		t.Fatalf("elapsed_ms = %v", snap["elapsed_ms"])
	}
	result, _ := snap["result"].(map[string]any)
	item, _ := result["item"].(map[string]any)
	if item["name"] != "后台导入模板" || item["kind"] != "pdf" {
		t.Fatalf("job result item = %+v", result)
	}
	// The job result is the same payload shape as the synchronous endpoint, so
	// the template is visible through the normal list surface.
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/templates?kind=pdf", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "后台导入模板") {
		t.Fatalf("templates list = %d %s", resp.StatusCode, body)
	}
}

func TestJobFailureKeepsServiceErrorEnvelope(t *testing.T) {
	f := newFixture(t)
	jobID := startJobRequest(t, f, "/app/api/v1/jobs/templates/import", map[string]any{
		"name":        "坏文件",
		"file_name":   "broken.docx",
		"file_base64": base64.StdEncoding.EncodeToString([]byte("not a word file")),
	})
	snap := pollJob(t, f, jobID)
	if snap["status"] != "failed" {
		t.Fatalf("job snapshot = %+v", snap)
	}
	errBody, _ := snap["error"].(map[string]any)
	if errBody["code"] != "invalid_file" || errBody["message"] == "" {
		t.Fatalf("job error = %+v", snap["error"])
	}
}

func TestJobStartRejectsInvalidRequestBody(t *testing.T) {
	f := newFixture(t)
	// A missing file is a request error, not a job: the synchronous handlers
	// answer it before anything is queued.
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/jobs/templates/import", map[string]any{
		"name": "空文件",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
}

func TestJobLookupUnknownReturnsNotFound(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/jobs/job_missing", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
}

func TestExcelPreviewJobRunsThroughRegistry(t *testing.T) {
	f := newFixture(t)
	// The blank entry workbook is a real .xlsx with headers but no data rows;
	// the preview job must finish cleanly and return the synchronous shape.
	workbook, err := f.service.TemplateWorkbook(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	jobID := startJobRequest(t, f, "/app/api/v1/jobs/excel/preview", map[string]any{
		"file_base64": base64.StdEncoding.EncodeToString(workbook),
		"options":     map[string]any{"mode": "upsert"},
	})
	snap := pollJob(t, f, jobID)
	if snap["status"] != "done" || snap["kind"] != "excel_preview" {
		t.Fatalf("job snapshot = %+v", snap)
	}
	result, _ := snap["result"].(map[string]any)
	if result["sheet"] == nil {
		t.Fatalf("job result = %+v", result)
	}
}

func TestExcelCommitJobRunsThroughRegistry(t *testing.T) {
	f := newFixture(t)
	workbook, err := f.service.TemplateWorkbook(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	jobID := startJobRequest(t, f, "/app/api/v1/jobs/excel/commit", map[string]any{
		"file_base64": base64.StdEncoding.EncodeToString(workbook),
		"options":     map[string]any{"mode": "upsert"},
	})
	snap := pollJob(t, f, jobID)
	if snap["status"] != "done" || snap["kind"] != "excel_commit" {
		t.Fatalf("job snapshot = %+v", snap)
	}
	result, _ := snap["result"].(map[string]any)
	if result["created"] == nil {
		t.Fatalf("job result = %+v", result)
	}
}

// importJobTemplate imports a PDF template through the synchronous endpoint so
// the job tests share the same fixture shape the UI starts from.
func importJobTemplate(t *testing.T, f *fixture) string {
	t.Helper()
	pdf := testTemplatePDF(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import", map[string]any{
		"name":        "后台生成模板",
		"file_name":   "jobs-template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import template status = %d body=%s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("imported template = %s", body)
	}
	return id
}

func TestContractPreviewAndGenerateJobs(t *testing.T) {
	f := newFixture(t)
	templateID := importJobTemplate(t, f)

	previewJob := startJobRequest(t, f, "/app/api/v1/jobs/contracts/"+f.contractID+"/preview",
		map[string]any{"template_id": templateID, "contract_id": f.contractID})
	previewSnap := pollJob(t, f, previewJob)
	if previewSnap["status"] != "done" || previewSnap["kind"] != "contract_preview" {
		t.Fatalf("preview job snapshot = %+v", previewSnap)
	}
	previewResult, _ := previewSnap["result"].(map[string]any)
	sha, _ := previewResult["preview_sha256"].(string)
	pdfBase64, _ := previewResult["pdf_base64"].(string)
	if sha == "" || pdfBase64 == "" {
		t.Fatalf("preview result = %+v", previewResult)
	}

	generateJob := startJobRequest(t, f, "/app/api/v1/jobs/contracts/"+f.contractID+"/generate",
		map[string]any{
			"template_id": templateID, "contract_id": f.contractID,
			"preview_sha256": sha,
		})
	generateSnap := pollJob(t, f, generateJob)
	if generateSnap["status"] != "done" || generateSnap["kind"] != "contract_generate" {
		t.Fatalf("generate job snapshot = %+v", generateSnap)
	}
	generateResult, _ := generateSnap["result"].(map[string]any)
	doc, _ := generateResult["item"].(map[string]any)
	if doc["status"] != "success" {
		t.Fatalf("generated document = %+v", generateResult)
	}
}

func TestBatchPreviewAndGenerateJobs(t *testing.T) {
	f := newFixture(t)
	templateID := importJobTemplate(t, f)

	previewJob := startJobRequest(t, f, "/app/api/v1/jobs/templates/"+templateID+"/batch-preview",
		map[string]any{"contract_ids": []string{f.contractID}})
	previewSnap := pollJob(t, f, previewJob)
	if previewSnap["status"] != "done" || previewSnap["kind"] != "batch_preview" {
		t.Fatalf("batch preview job snapshot = %+v", previewSnap)
	}
	previewResult, _ := previewSnap["result"].(map[string]any)
	items, _ := previewResult["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("batch preview result = %+v", previewResult)
	}
	first, _ := items[0].(map[string]any)
	sha, _ := first["preview_sha256"].(string)
	if sha == "" {
		t.Fatalf("preflight item = %+v", first)
	}

	generateJob := startJobRequest(t, f, "/app/api/v1/jobs/templates/"+templateID+"/batch-generate",
		map[string]any{"items": []map[string]any{{
			"contract_id": f.contractID, "preview_sha256": sha,
		}}})
	generateSnap := pollJob(t, f, generateJob)
	if generateSnap["status"] != "done" || generateSnap["kind"] != "batch_generate" {
		t.Fatalf("batch generate job snapshot = %+v", generateSnap)
	}
	if total, _ := generateSnap["total"].(float64); total != 1 {
		t.Fatalf("batch generate total = %v", generateSnap["total"])
	}
	generateResult, _ := generateSnap["result"].(map[string]any)
	summary, _ := generateResult["summary"].(map[string]any)
	if summary["success"] != float64(1) {
		t.Fatalf("batch generate result = %+v", generateResult)
	}
}
