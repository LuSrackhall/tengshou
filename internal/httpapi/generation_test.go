package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// b4HTTPSetup imports a PDF template with a contract-number field and a
// party-B signature placement, and pins a content-versioned signature so the
// content gate can be exercised.
func b4HTTPSetup(t *testing.T, f *fixture) (templateID string, digest string) {
	t.Helper()
	ctx := context.Background()
	pngBytes, err := base64.StdEncoding.DecodeString(sealTestPNG(t, 80, 28))
	if err != nil {
		t.Fatal(err)
	}
	sigPath := filepath.Join(f.dataDir, "assets", "sig.png")
	if err := os.MkdirAll(filepath.Dir(sigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	pdf := testTemplatePDF(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/templates/import", map[string]any{
		"name": "B4 接口模板", "file_name": "b4-template.pdf",
		"file_base64": base64.StdEncoding.EncodeToString(pdf),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, body)
	}
	templateID = decodeAppItem(t, body)["id"].(string)
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/templates/"+templateID+"/mapping",
		map[string]any{"placements": []map[string]any{
			{"id": "no", "page": 1, "x": 110, "y": 150, "w": 220, "h": 22,
				"type": "field", "field": "contract.contract_no"},
			{"id": "sig", "page": 1, "x": 300, "y": 600, "w": 140, "h": 60,
				"type": "signature", "party": "party_b"},
		}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mapping status=%d body=%s", resp.StatusCode, body)
	}
	templateID = decodeAppItem(t, body)["id"].(string)

	contract, err := f.reader.GetContract(ctx, f.contractID)
	if err != nil {
		t.Fatal(err)
	}
	digest, err = service.ContentSHA256(contract)
	if err != nil {
		t.Fatal(err)
	}
	sig := model.Signature{
		PartyID: f.partyBID, ContractID: f.contractID, SignerName: "张三",
		AssetPath: "assets/sig.png", SHA256: "b4sigsha", ContentSHA256: digest,
		WidthPx: 80, HeightPx: 80, CapturedAt: time.Now().UTC().Add(time.Minute),
		Source: model.SignatureSourceLANQR,
	}
	if err := f.store.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}
	return templateID, digest
}

func TestGenerationHistoryContentGateAndBatchAPI(t *testing.T) {
	f := newFixture(t)
	templateID, contentSHA := b4HTTPSetup(t, f)

	// Preflight reports readiness plus the digest generation must echo.
	resp, body := f.appRequest(t, http.MethodPost,
		"/app/api/v1/templates/"+templateID+"/preflight",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preflight status=%d body=%s", resp.StatusCode, body)
	}
	var preflight struct {
		Ready      bool   `json:"ready"`
		Status     string `json:"status"`
		Preview    string `json:"preview_sha256"`
		Content    string `json:"content_sha256"`
		Signatures []struct {
			SignatureID string `json:"signature_id"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(body, &preflight); err != nil {
		t.Fatal(err)
	}
	if !preflight.Ready || preflight.Status != service.PreflightReady ||
		len(preflight.Preview) != 64 || preflight.Content != contentSHA ||
		len(preflight.Signatures) != 1 {
		t.Fatalf("preflight = %s", body)
	}

	// Preview then generate records a full success entry.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/preview",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", resp.StatusCode, body)
	}
	var preview struct {
		PreviewSHA256 string `json:"preview_sha256"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PreviewSHA256 != preflight.Preview {
		t.Fatalf("preflight/preview digest mismatch: %q %q", preflight.Preview, preview.PreviewSHA256)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{"contract_id": f.contractID, "preview_sha256": preview.PreviewSHA256, "kind": "initial"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", resp.StatusCode, body)
	}
	first := decodeAppItem(t, body)
	if first["status"] != model.DocumentStatusSuccess || first["kind"] != model.DocumentKindInitial ||
		first["operator"] != "tester" || first["content_sha256"] != contentSHA {
		t.Fatalf("document = %s", body)
	}
	firstID := first["id"].(string)

	// A second output is a regenerate pointing at the previous document.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/preview",
		map[string]any{"contract_id": f.contractID}, nil)
	if err := json.Unmarshal(body, &preview); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("preview2 status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{"contract_id": f.contractID, "preview_sha256": preview.PreviewSHA256}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("regenerate status=%d body=%s", resp.StatusCode, body)
	}
	second := decodeAppItem(t, body)
	if second["kind"] != model.DocumentKindRegenerate || second["source_document_id"] != firstID {
		t.Fatalf("regenerate document = %s", body)
	}

	// Batch preview returns per-item readiness, a sample PDF and digests.
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/templates/"+templateID+"/batch/preview",
		map[string]any{"contract_ids": []string{f.contractID}, "sample": true}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch preview status=%d body=%s", resp.StatusCode, body)
	}
	var batchPreview struct {
		PDFBase64 string `json:"pdf_base64"`
		Summary   struct {
			Total   int `json:"total"`
			Ready   int `json:"ready"`
			Blocked int `json:"blocked"`
		} `json:"summary"`
		Items []struct {
			ContractID    string `json:"contract_id"`
			Ready         bool   `json:"ready"`
			PreviewSHA256 string `json:"preview_sha256"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &batchPreview); err != nil {
		t.Fatal(err)
	}
	if batchPreview.PDFBase64 == "" || batchPreview.Summary.Total != 1 ||
		batchPreview.Summary.Ready != 1 || !batchPreview.Items[0].Ready {
		t.Fatalf("batch preview = %s", body)
	}
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/templates/"+templateID+"/batch/generate",
		map[string]any{"items": []map[string]any{{
			"contract_id":    f.contractID,
			"preview_sha256": batchPreview.Items[0].PreviewSHA256,
		}}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch generate status=%d body=%s", resp.StatusCode, body)
	}
	var batchResult struct {
		BatchID string `json:"batch_id"`
		Summary struct {
			Total   int `json:"total"`
			Success int `json:"success"`
			Failed  int `json:"failed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &batchResult); err != nil {
		t.Fatal(err)
	}
	if batchResult.BatchID == "" || batchResult.Summary.Success != 1 {
		t.Fatalf("batch result = %s", body)
	}

	// Generation history can be queried by contract, template and batch.
	resp, body = f.get(t, "/app/api/v1/documents?contract_id="+f.contractID+"&status=success", true)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), model.DocumentKindRegenerate) {
		t.Fatalf("documents by contract status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/documents?batch_id="+batchResult.BatchID, true)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), batchResult.BatchID) {
		t.Fatalf("documents by batch status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/documents?template_id="+templateID, true)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), templateID) {
		t.Fatalf("documents by template status=%d body=%s", resp.StatusCode, body)
	}

	// Changing the contract content invalidates the retained signature.
	detailResp, detailBody := f.get(t, "/app/api/v1/contracts/"+f.contractID, true)
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d", detailResp.StatusCode)
	}
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/contracts/"+f.contractID,
		map[string]any{
			"contract_no": "", "year": 2026, "title": "改名后的合同",
			"party_b_id": f.partyBID, "amount": 1200, "status": "active",
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update contract status=%d body=%s (detail=%s)", resp.StatusCode, body, detailBody)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/preview",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview after edit status=%d body=%s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{"contract_id": f.contractID, "preview_sha256": preview.PreviewSHA256}, nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "content_changed") {
		t.Fatalf("content gate status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/documents?contract_id="+f.contractID+"&status=failed", true)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), model.DocumentStatusFailed) {
		t.Fatalf("failed documents status=%d body=%s", resp.StatusCode, body)
	}

	// Explicit confirmation keeps the old signature and audited the action.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/templates/"+templateID+"/generate",
		map[string]any{
			"contract_id": f.contractID, "preview_sha256": preview.PreviewSHA256,
			"allow_stale_signature": true,
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirmed generate status=%d body=%s", resp.StatusCode, body)
	}
}
