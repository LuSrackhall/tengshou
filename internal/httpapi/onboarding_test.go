package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/model"
)

type onboardingStepView struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Done       bool   `json:"done"`
	DoneAt     string `json:"done_at"`
	Detail     string `json:"detail"`
	Action     string `json:"action"`
	DataBacked bool   `json:"data_backed"`
}

type onboardingView struct {
	FirstRun    bool                 `json:"first_run"`
	Completed   bool                 `json:"completed"`
	Skipped     bool                 `json:"skipped"`
	CompletedAt string               `json:"completed_at"`
	SkippedAt   string               `json:"skipped_at"`
	ShowGuide   bool                 `json:"show_guide"`
	Steps       []onboardingStepView `json:"steps"`
	Facts       struct {
		Templates           int `json:"templates"`
		CalibratedTemplates int `json:"calibrated_templates"`
		Contracts           int `json:"contracts"`
	} `json:"facts"`
}

func decodeOnboarding(t *testing.T, body []byte) onboardingView {
	t.Helper()
	var view onboardingView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode onboarding: %v body=%s", err, body)
	}
	return view
}

func onboardingStep(t *testing.T, view onboardingView, id string) onboardingStepView {
	t.Helper()
	for _, step := range view.Steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("step %q missing from %+v", id, view.Steps)
	return onboardingStepView{}
}

type checkItemView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Action string `json:"action"`
	Target string `json:"target"`
}

type checkView struct {
	CheckedAt string          `json:"checked_at"`
	Items     []checkItemView `json:"items"`
	Summary   map[string]int  `json:"summary"`
}

func decodeCheck(t *testing.T, body []byte) checkView {
	t.Helper()
	var view checkView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode check: %v body=%s", err, body)
	}
	if len(view.Items) == 0 {
		t.Fatalf("check returned no items: %s", body)
	}
	return view
}

func checkItem(t *testing.T, view checkView, id string) checkItemView {
	t.Helper()
	for _, item := range view.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("check item %q missing from %+v", id, view.Items)
	return checkItemView{}
}

// TestOnboardingFirstRunAndGuideState covers first-run detection, the shared
// bootstrap payload and the config-only bookkeeping (complete / skip / reopen /
// sample step). None of these calls may write business data.
func TestOnboardingFirstRunAndGuideState(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{empty: true, allowLAN: false})
	paths, err := config.ResolvePaths(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}

	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("onboarding status=%d body=%s", resp.StatusCode, body)
	}
	view := decodeOnboarding(t, body)
	if !view.FirstRun || !view.ShowGuide || view.Completed || view.Skipped {
		t.Fatalf("fresh guide state = %+v", view)
	}
	if len(view.Steps) != 5 {
		t.Fatalf("steps = %+v", view.Steps)
	}
	for _, step := range view.Steps {
		if step.Done {
			t.Fatalf("step %s should not be done on a fresh install", step.ID)
		}
		if step.Action == "" || step.Label == "" {
			t.Fatalf("step %s lacks label/action: %+v", step.ID, step)
		}
	}

	// Bootstrap carries the same guide state for the initial page load.
	resp, body = f.get(t, "/app/api/v1/bootstrap", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", resp.StatusCode, body)
	}
	var boot struct {
		Onboarding onboardingView `json:"onboarding"`
	}
	if err := json.Unmarshal(body, &boot); err != nil {
		t.Fatal(err)
	}
	if !boot.Onboarding.FirstRun || !boot.Onboarding.ShowGuide {
		t.Fatalf("bootstrap onboarding = %+v", boot.Onboarding)
	}

	// Recording a sample-only step persists to config.json only.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "step", "step": "sample_preview"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark step status=%d body=%s", resp.StatusCode, body)
	}
	view = decodeOnboarding(t, body)
	step := onboardingStep(t, view, "sample_preview")
	if !step.Done || step.DoneAt == "" {
		t.Fatalf("sample_preview step = %+v", step)
	}
	if view.Completed || view.Skipped {
		t.Fatalf("marking a step must not complete the guide: %+v", view)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Onboarding.Steps["sample_preview"] == "" {
		t.Fatalf("step not persisted: %+v", cfg.Onboarding)
	}
	stats, err := f.reader.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Contracts != 0 || stats.Parties != 0 || stats.Templates != 0 || stats.Documents != 0 {
		t.Fatalf("guide bookkeeping wrote business data: %+v", stats)
	}

	// Complete hides the automatic guide even while the vault is still empty.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "complete"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", resp.StatusCode, body)
	}
	view = decodeOnboarding(t, body)
	if !view.Completed || view.CompletedAt == "" || view.ShowGuide {
		t.Fatalf("completed view = %+v", view)
	}
	cfg, err = config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Onboarding.Completed {
		t.Fatal("completed state not persisted")
	}

	// Reopen makes the guide available again (manual restart from the nav).
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "reopen"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reopen status=%d body=%s", resp.StatusCode, body)
	}
	view = decodeOnboarding(t, body)
	if view.Completed || view.Skipped || !view.ShowGuide {
		t.Fatalf("reopened view = %+v", view)
	}

	// Skip stops the automatic guide until the user reopens it.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "skip"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("skip status=%d body=%s", resp.StatusCode, body)
	}
	view = decodeOnboarding(t, body)
	if !view.Skipped || view.SkippedAt == "" || view.ShowGuide || view.Completed {
		t.Fatalf("skipped view = %+v", view)
	}

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "nonsense"}, nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_action") {
		t.Fatalf("invalid action status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
		map[string]any{"action": "step", "step": "contract_create"}, nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_step") {
		t.Fatalf("invalid step status=%d body=%s", resp.StatusCode, body)
	}
}

// TestOnboardingStepsFromData checks that data-backed steps flip without any
// bookkeeping call once a template is imported, calibrated and a contract
// exists.
func TestOnboardingStepsFromData(t *testing.T) {
	f := newFixture(t)
	templateID := importTestTemplate(t, f, "引导模板")
	saveTestMapping(t, f, templateID, []map[string]any{
		{"id": "no", "page": 1, "x": 60, "y": 100, "w": 220, "h": 20, "type": "field", "field": "contract.contract_no"},
		{"id": "title", "page": 1, "x": 60, "y": 130, "w": 260, "h": 20, "type": "field", "field": "contract.title"},
	})

	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("onboarding status=%d body=%s", resp.StatusCode, body)
	}
	view := decodeOnboarding(t, body)
	if view.FirstRun || view.ShowGuide {
		t.Fatalf("vault with data must not look like a fresh install: %+v", view)
	}
	if view.Facts.Templates == 0 || view.Facts.CalibratedTemplates == 0 || view.Facts.Contracts == 0 {
		t.Fatalf("facts = %+v", view.Facts)
	}
	for _, id := range []string{"template_import", "template_calibrate", "contract_create"} {
		if step := onboardingStep(t, view, id); !step.Done {
			t.Fatalf("step %s should be done: %+v", id, step)
		}
	}
	for _, id := range []string{"sample_preview", "trial_pdf"} {
		if step := onboardingStep(t, view, id); step.Done {
			t.Fatalf("sample step %s needs explicit bookkeeping: %+v", id, step)
		}
	}
}

// TestOnboardingSelfCheck covers the read-only /onboarding/check endpoint:
// per-item statuses, actionable failures, LAN detection and backup presence.
func TestOnboardingSelfCheck(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{empty: true, allowLAN: false})
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding/check", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check status=%d body=%s", resp.StatusCode, body)
	}
	view := decodeCheck(t, body)
	if view.CheckedAt == "" {
		t.Fatal("check response has no checked_at")
	}
	for _, id := range []string{"data_dir", "database", "converter", "signature_lan", "fields", "template", "sample", "backup", "disk"} {
		item := checkItem(t, view, id)
		if item.Label == "" {
			t.Fatalf("item %s has no label: %+v", id, item)
		}
		switch item.Status {
		case "ok", "warn", "error":
		default:
			t.Fatalf("item %s status = %q", id, item.Status)
		}
		if item.Status != "ok" && item.Action == "" {
			t.Fatalf("failing item %s has no next step: %+v", id, item)
		}
	}
	if item := checkItem(t, view, "data_dir"); item.Status != "ok" {
		t.Fatalf("data_dir = %+v", item)
	}
	if item := checkItem(t, view, "database"); item.Status != "ok" {
		t.Fatalf("database = %+v", item)
	}
	if item := checkItem(t, view, "template"); item.Status != "warn" || item.Target != "templates" {
		t.Fatalf("template = %+v", item)
	}
	if item := checkItem(t, view, "signature_lan"); item.Status != "warn" {
		t.Fatalf("signature_lan = %+v", item)
	}
	if item := checkItem(t, view, "backup"); item.Status != "warn" {
		t.Fatalf("backup = %+v", item)
	}
	if view.Summary["ok"] == 0 || view.Summary["warn"] == 0 {
		t.Fatalf("summary = %+v", view.Summary)
	}

	// A backup file turns the backup item green.
	backups := filepath.Join(f.dataDir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, "tengshou-backup-20260101-000000.zip"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding/check", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check status=%d body=%s", resp.StatusCode, body)
	}
	view = decodeCheck(t, body)
	if item := checkItem(t, view, "backup"); item.Status != "ok" || !strings.Contains(item.Detail, "tengshou-backup") {
		t.Fatalf("backup after export = %+v", item)
	}

	if runtime.GOOS != "windows" {
		mode := os.FileMode(0o500)
		if err := os.Chmod(f.dataDir, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(f.dataDir, 0o700) })
		resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding/check", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("check on read-only dir status=%d body=%s", resp.StatusCode, body)
		}
		view = decodeCheck(t, body)
		if item := checkItem(t, view, "data_dir"); item.Status != "error" || item.Action == "" {
			t.Fatalf("read-only data_dir = %+v", item)
		}
	}
}

// TestOnboardingSelfCheckLAN verifies the LAN signature item when the service
// is reachable from a LAN address.
func TestOnboardingSelfCheckLAN(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{
		empty: true, allowLAN: true, lanHosts: []string{"192.168.1.20"},
	})
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/onboarding/check", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check status=%d body=%s", resp.StatusCode, body)
	}
	item := checkItem(t, decodeCheck(t, body), "signature_lan")
	if item.Status != "ok" || !strings.Contains(item.Detail, "192.168.1.20") {
		t.Fatalf("signature_lan = %+v", item)
	}
}

// TestTemplateCheckEndpoint covers the template wizard validation: unknown and
// archived bindings, unbound required fields, and missing signature/seal
// positions. The check is read-only.
func TestTemplateCheckEndpoint(t *testing.T) {
	f := newFixture(t)
	templateID := importTestTemplate(t, f, "体检模板")
	fieldID := createRequiredField(t, f, "landlord_id", "房东证件号", true)
	archivedFieldID := createRequiredField(t, f, "deposit_note", "押金说明", false)

	version := saveTestMapping(t, f, templateID, []map[string]any{
		{"id": "no", "page": 1, "x": 60, "y": 100, "w": 220, "h": 20, "type": "field", "field": "contract.contract_no"},
		{"id": "year", "page": 1, "x": 60, "y": 130, "w": 120, "h": 20, "type": "field", "field": "contract.year"},
		{"id": "title", "page": 1, "x": 60, "y": 160, "w": 260, "h": 20, "type": "field", "field": "contract.title"},
		{"id": "party", "page": 1, "x": 60, "y": 190, "w": 220, "h": 20, "type": "field", "field": "contract.party_b"},
		{"id": "party_name", "page": 1, "x": 60, "y": 220, "w": 220, "h": 20, "type": "field", "field": "party_b.name"},
		{"id": "deposit", "page": 1, "x": 60, "y": 250, "w": 220, "h": 20, "type": "field", "field": "contract.data.deposit_note"},
		{"id": "sig", "page": 1, "x": 60, "y": 700, "w": 130, "h": 50, "type": "signature", "party": "party_b"},
		{"id": "seal", "page": 1, "x": 350, "y": 700, "w": 110, "h": 110, "type": "seal", "seal_mode": "contract"},
	})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/fields/"+archivedFieldID+"/archive", map[string]any{}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive field status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/templates/"+version+"/check", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("template check status=%d body=%s", resp.StatusCode, body)
	}
	var check struct {
		TemplateID          string `json:"template_id"`
		PlacementCount      int    `json:"placement_count"`
		FieldPlacements     int    `json:"field_placements"`
		SignaturePlacements int    `json:"signature_placements"`
		SealPlacements      int    `json:"seal_placements"`
		OK                  bool   `json:"ok"`
		Issues              []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Field    string `json:"field"`
			Message  string `json:"message"`
			Action   string `json:"action"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(body, &check); err != nil {
		t.Fatal(err)
	}
	if check.TemplateID != version || check.PlacementCount != 8 || check.FieldPlacements != 6 ||
		check.SignaturePlacements != 1 || check.SealPlacements != 1 {
		t.Fatalf("check counts = %+v", check)
	}
	if !check.OK {
		t.Fatalf("warnings must keep ok=true: %+v", check)
	}
	codes := map[string]string{}
	for _, issue := range check.Issues {
		codes[issue.Code] = issue.Severity
		if issue.Action == "" || issue.Message == "" {
			t.Fatalf("issue lacks action/message: %+v", issue)
		}
	}
	if codes["archived_field"] != "warn" {
		t.Fatalf("archived field not reported: %+v", check.Issues)
	}
	if codes["missing_required_field"] != "warn" {
		t.Fatalf("unbound required field not reported: %+v", check.Issues)
	}
	if _, exists := codes["no_signature_placement"]; exists {
		t.Fatalf("signature placement was present: %+v", check.Issues)
	}
	if _, exists := codes["no_seal_placement"]; exists {
		t.Fatalf("seal placement was present: %+v", check.Issues)
	}
	_ = fieldID

	// A template record from an older version may reference a field that no
	// longer exists; the check reports it as an error and points to the fix.
	ghost := model.Template{
		Name: "旧模板", Version: 1, Kind: model.TemplateKindPDF, IsActive: true,
		PDFPath: "templates/pdf/fake/template.pdf", PageCount: 1,
		Placements: []model.Placement{{
			ID: "ghost", Page: 1, X: 60, Y: 60, W: 200, H: 20,
			Type: model.PlacementField, Field: "contract.data.ghost_field",
		}},
	}
	if err := f.store.PutTemplate(context.Background(), &ghost); err != nil {
		t.Fatal(err)
	}
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/templates/"+ghost.ID+"/check", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ghost check status=%d body=%s", resp.StatusCode, body)
	}
	var ghostCheck struct {
		OK     bool `json:"ok"`
		Issues []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Action   string `json:"action"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(body, &ghostCheck); err != nil {
		t.Fatal(err)
	}
	if ghostCheck.OK {
		t.Fatal("unknown field must make ok=false")
	}
	found := false
	for _, issue := range ghostCheck.Issues {
		if issue.Code == "unknown_field" && issue.Severity == "error" && issue.Action != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown field not reported: %+v", ghostCheck.Issues)
	}
}

// TestSampleTrialDoesNotWriteBusinessData is the B8 guard rail: the sample
// preview path and guide bookkeeping never create contracts, parties,
// templates, documents or template versions.
func TestSampleTrialDoesNotWriteBusinessData(t *testing.T) {
	f := newFixture(t)
	templateID := importTestTemplate(t, f, "样例模板")
	version := saveTestMapping(t, f, templateID, []map[string]any{
		{"id": "no", "page": 1, "x": 60, "y": 100, "w": 220, "h": 20, "type": "field", "field": "contract.contract_no"},
	})
	before, err := f.reader.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeDocs, err := f.reader.ListDocuments(context.Background(), model.ListFilter{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+version+"/preview",
		map[string]any{"sample": true}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sample preview status=%d body=%s", resp.StatusCode, body)
	}
	var preview struct {
		PDFBase64 string `json:"pdf_base64"`
		Sample    bool   `json:"sample"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PDFBase64 == "" || !preview.Sample {
		t.Fatalf("sample preview = %+v", preview)
	}
	if _, err := base64.StdEncoding.DecodeString(preview.PDFBase64); err != nil {
		t.Fatal(err)
	}

	// Marking the two sample-only steps is config bookkeeping, not data.
	for _, step := range []string{"sample_preview", "trial_pdf"} {
		resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/onboarding/state",
			map[string]any{"action": "step", "step": step}, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mark %s status=%d body=%s", step, resp.StatusCode, body)
		}
	}

	after, err := f.reader.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("sample flow changed stats: before=%+v after=%+v", before, after)
	}
	afterDocs, err := f.reader.ListDocuments(context.Background(), model.ListFilter{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if afterDocs.Total != beforeDocs.Total {
		t.Fatalf("sample flow wrote output documents: before=%d after=%d", beforeDocs.Total, afterDocs.Total)
	}
}

func importTestTemplate(t *testing.T, f *fixture, name string) string {
	t.Helper()
	pdf := testTemplatePDF(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import", map[string]any{
		"name":        name,
		"file_name":   "template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import template status=%d body=%s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("imported template has no id: %s", body)
	}
	return id
}

func saveTestMapping(t *testing.T, f *fixture, templateID string, placements []map[string]any) string {
	t.Helper()
	resp, body := f.appRequest(t, http.MethodPut, "/app/api/v1/templates/"+templateID+"/mapping",
		map[string]any{"placements": placements}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save mapping status=%d body=%s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("saved template has no id: %s", body)
	}
	return id
}

func createRequiredField(t *testing.T, f *fixture, key, label string, required bool) string {
	t.Helper()
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": key, "label": label, "type": "text",
		"required": required, "in_ledger": true, "in_template": true,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create field %s status=%d body=%s", key, resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("created field has no id: %s", body)
	}
	return id
}
