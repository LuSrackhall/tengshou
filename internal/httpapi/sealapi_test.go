package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

func sealTestPNG(t *testing.T, size, radius int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := (x-cx)*(x-cx) + (y-cy)*(y-cy)
			if d <= radius*radius && d >= (radius-10)*(radius-10) {
				img.SetNRGBA(x, y, color.NRGBA{190, 30, 30, 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestSealImportPreviewAttachReplaceDelete(t *testing.T) {
	f := newFixture(t)

	resp, raw := f.appRequest(t, http.MethodPost, "/app/api/v1/seals", map[string]any{
		"name":        "合作社公章",
		"party_id":    f.partyID,
		"note":        "测试",
		"file_base64": sealTestPNG(t, 260, 100),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", resp.StatusCode, raw)
	}
	item := decodeAppItem(t, raw)
	sealID, _ := item["id"].(string)
	assetPath, _ := item["asset_path"].(string)
	if sealID == "" || !strings.HasPrefix(assetPath, "assets/seals/") {
		t.Fatalf("import item = %v", item)
	}
	sha1, _ := item["sha256"].(string)
	if len(sha1) != 64 {
		t.Fatalf("sha256 = %q", sha1)
	}

	// The list endpoint and the PNG preview work for the app UI.
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/seals?q=合作社", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.StatusCode, raw)
	}
	var list model.List[model.Seal]
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].ID != sealID {
		t.Fatalf("seal list = %+v", list)
	}
	resp, body := f.get(t, "/app/api/v1/seals/"+sealID+"/image", false)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("image status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("preview is not a png: %v", err)
	}

	// Attach to the fixture contract (single), then confirm the contract detail
	// and the reverse index both expose the link.
	resp, raw = f.appRequest(t, http.MethodPost, "/app/api/v1/seals/"+sealID+"/attach",
		map[string]any{"contract_ids": []string{f.contractID}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach status = %d body=%s", resp.StatusCode, raw)
	}
	var attached map[string]any
	_ = json.Unmarshal(raw, &attached)
	if attached["attached"] != float64(1) {
		t.Fatalf("attach result = %s", raw)
	}
	resp, raw = f.get(t, "/api/v1/contracts/"+f.contractID, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("contract detail status = %d body=%s", resp.StatusCode, raw)
	}
	var detail model.ContractDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Seals) != 1 || detail.Seals[0].ID != sealID {
		t.Fatalf("contract detail seals = %+v", detail.Seals)
	}
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/seals/"+sealID+"/contracts", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), f.contractID) {
		t.Fatalf("seal contracts status=%d body=%s", resp.StatusCode, raw)
	}

	// Replacing the picture changes the digest and keeps the association.
	resp, raw = f.appRequest(t, http.MethodPost, "/app/api/v1/seals/"+sealID+"/replace",
		map[string]any{"file_base64": sealTestPNG(t, 300, 120)}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace status = %d body=%s", resp.StatusCode, raw)
	}
	replaced := decodeAppItem(t, raw)
	sha2, _ := replaced["sha256"].(string)
	if sha2 == "" || sha2 == sha1 {
		t.Fatalf("replace sha = %q (old %q)", sha2, sha1)
	}
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/seals/"+sealID+"/contracts", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), f.contractID) {
		t.Fatalf("links lost after replace: %s", raw)
	}

	if err := f.service.DeleteSeal(context.Background(), sealID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp, _ = f.appRequest(t, http.MethodGet, "/app/api/v1/seals/"+sealID, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted seal status = %d", resp.StatusCode)
	}
	// The read-only detail endpoint reflects the removed association.
	resp, raw = f.get(t, "/api/v1/contracts/"+f.contractID, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail after delete status = %d", resp.StatusCode)
	}
	var after model.ContractDetail
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Seals) != 0 {
		t.Fatalf("deleted seal still linked: %+v", after.Seals)
	}
}

func TestSealImportValidation(t *testing.T) {
	f := newFixture(t)
	resp, raw := f.appRequest(t, http.MethodPost, "/app/api/v1/seals", map[string]any{
		"name": "公章", "file_base64": "not-an-image",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad image status = %d body=%s", resp.StatusCode, raw)
	}
	resp, raw = f.appRequest(t, http.MethodPost, "/app/api/v1/seals", map[string]any{
		"name": "公章", "file_base64": sealTestPNG(t, 260, 100),
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", resp.StatusCode, raw)
	}
	sealID := decodeAppItem(t, raw)["id"].(string)
	resp, raw = f.appRequest(t, http.MethodPost, "/app/api/v1/seals/"+sealID+"/attach",
		map[string]any{"contract_ids": []string{}}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty attach status = %d body=%s", resp.StatusCode, raw)
	}
	resp, raw = f.appRequest(t, http.MethodPost, "/app/api/v1/seals/"+sealID+"/attach",
		map[string]any{"contract_ids": []string{"ctr_missing"}}, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing contract status = %d body=%s", resp.StatusCode, raw)
	}
}
