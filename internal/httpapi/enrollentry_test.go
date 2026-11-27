package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// te-48hkl: party archive seal and signature-stamp pictures are uploaded on
// the desktop surface, served back as PNG, and can be disabled without
// deleting the immutable asset.
func TestAppPartySealAndSignatureSealImages(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "档案公章测试", "kind": "organization", "id_number": "91330100TEST000001",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	party := decodeAppItem(t, body)
	id, _ := party["id"].(string)

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+id+"/seal",
		map[string]any{"image": photoDataURL(t, 160, 140)}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload seal: %d %s", resp.StatusCode, body)
	}
	updated := decodeAppItem(t, body)
	if updated["seal_asset_id"] == "" || updated["seal_source"] != model.PartyAssetSourceAppUpload {
		t.Fatalf("seal metadata missing: %v", updated)
	}
	resp, imageBody := f.appRequest(t, http.MethodGet, "/app/api/v1/parties/"+id+"/seal", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || len(imageBody) == 0 {
		t.Fatalf("serve seal: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+id+"/signature-seal",
		map[string]any{"image": photoDataURL(t, 120, 120)}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload signature seal: %d %s", resp.StatusCode, body)
	}
	updated = decodeAppItem(t, body)
	if updated["signature_seal_id"] == "" || updated["signature_seal_source"] != model.PartyAssetSourceAppUpload {
		t.Fatalf("signature seal metadata missing: %v", updated)
	}
	resp, imageBody = f.appRequest(t, http.MethodGet, "/app/api/v1/parties/"+id+"/signature-seal", nil, nil)
	if resp.StatusCode != http.StatusOK || len(imageBody) == 0 {
		t.Fatalf("serve signature seal: %d", resp.StatusCode)
	}

	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/parties/"+id+"/seal", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable seal: %d %s", resp.StatusCode, body)
	}
	if cleared := decodeAppItem(t, body); cleared["seal_asset_id"] != nil && cleared["seal_asset_id"] != "" {
		t.Fatalf("seal not cleared: %v", cleared)
	}
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/parties/"+id+"/signature-seal", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable signature seal: %d %s", resp.StatusCode, body)
	}
	if cleared := decodeAppItem(t, body); cleared["signature_seal_id"] != nil && cleared["signature_seal_id"] != "" {
		t.Fatalf("signature seal not cleared: %v", cleared)
	}
}

// The long-lived poster token is reusable by many tenants, accepts a
// signature stamp instead of a handwritten signature, records the company
// seal, and stops working after rotate/disable.
func TestAppFixedEnrollEntryFlow(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-entry/rotate", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate entry: %d %s", resp.StatusCode, body)
	}
	entry := decodeAppItem(t, body)
	token, _ := entry["token"].(string)
	if len(token) != 64 || entry["enabled"] != true {
		t.Fatalf("entry view = %v", entry)
	}
	if urls, _ := entry["urls"].([]any); len(urls) == 0 {
		t.Fatalf("entry has no urls: %v", entry)
	}

	page := signRaw(t, f, http.MethodGet, "/enroll/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "enrollCard") {
		t.Fatalf("fixed entry page = %d %s", page.Code, page.Body.String())
	}

	submit := func(name, idNumber string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(map[string]any{
			"name":           name,
			"id_number":      idNumber,
			"phone":          "13800002222",
			"id_front":       photoDataURL(t, 50, 30),
			"id_back":        photoDataURL(t, 50, 30),
			"signature_seal": photoDataURL(t, 120, 120),
			"seal":           photoDataURL(t, 150, 150),
		})
		if err != nil {
			t.Fatal(err)
		}
		return signRaw(t, f, http.MethodPost, "/enroll/"+token+"/submit", payload, "192.168.1.23:5000",
			map[string]string{"Content-Type": "application/json", "X-Tengshou-Sign": "1"})
	}

	if rec := submit("海报租户甲", "330102198001015001"); rec.Code != http.StatusOK {
		t.Fatalf("first fixed submit: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := f.reader.FindPartiesByIDNumber(context.Background(), "330102198001015001")
	if err != nil || len(stored) != 1 {
		t.Fatalf("enrolled party = %v err=%v", stored, err)
	}
	party := stored[0]
	if party.SignatureID != "" || party.SignatureSealID == "" || party.SealAssetID == "" {
		t.Fatalf("fixed enrollment assets = %+v", party)
	}
	if party.SealSource != model.PartyAssetSourceLANEnroll ||
		party.SignatureSealSource != model.PartyAssetSourceLANEnroll {
		t.Fatalf("fixed enrollment sources = %q/%q", party.SealSource, party.SignatureSealSource)
	}
	// The poster token is not one-shot: the next tenant can use the same link.
	if rec := submit("海报租户乙", "330102198001015002"); rec.Code != http.StatusOK {
		t.Fatalf("second fixed submit: %d %s", rec.Code, rec.Body.String())
	}

	// Disable: the printed poster stops working immediately.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-entry/disable", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable entry: %d %s", resp.StatusCode, body)
	}
	if got := signRaw(t, f, http.MethodGet, "/enroll/"+token, nil, "192.168.1.23:5000", nil); got.Code != http.StatusGone {
		t.Fatalf("disabled page = %d", got.Code)
	}
	if got := submit("禁用后租户", "330102198001015003"); got.Code != http.StatusGone {
		t.Fatalf("disabled submit = %d %s", got.Code, got.Body.String())
	}

	// Rotate: the old poster stays dead, the new token works.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-entry/rotate", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate again: %d %s", resp.StatusCode, body)
	}
	newToken, _ := decodeAppItem(t, body)["token"].(string)
	if newToken == token {
		t.Fatal("rotate should issue a different token")
	}
	if got := signRaw(t, f, http.MethodGet, "/enroll/"+newToken, nil, "192.168.1.23:5000", nil); got.Code != http.StatusOK {
		t.Fatalf("new entry page = %d", got.Code)
	}
}

func TestAppArchiveReuseSettings(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/settings/archive-reuse", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"enabled":true`) || !strings.Contains(string(body), "风险") {
		t.Fatalf("default settings = %s", body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/settings/archive-reuse",
		map[string]any{"enabled": false}, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("disable settings: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/settings/archive-reuse", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("persisted settings = %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/settings/archive-reuse", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing enabled = %d %s", resp.StatusCode, body)
	}
}

// Reusing an archive signature stamp reports the archive_seal source, and an
// unknown kind is rejected before touching the contract.
func TestAppReusePartySignatureSeal(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "签名章复用租户", "kind": "person", "id_number": "110101199001019999",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	partyID, _ := decodeAppItem(t, body)["id"].(string)
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+partyID+"/signature-seal",
		map[string]any{"image": photoDataURL(t, 120, 120)}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload signature seal: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"title": "签名章复用合同", "party_b_id": partyID, "status": "draft",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create contract: %d %s", resp.StatusCode, body)
	}
	contractID, _ := decodeAppItem(t, body)["id"].(string)

	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contractID+"/signature-reuse",
		map[string]any{"party_id": partyID, "kind": "unknown"}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown kind = %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contractID+"/signature-reuse",
		map[string]any{"party_id": partyID, "kind": "archive_seal"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reuse signature seal: %d %s", resp.StatusCode, body)
	}
	if item := decodeAppItem(t, body); item["source"] != model.SignatureSourceArchiveSeal {
		t.Fatalf("reuse source = %v", item["source"])
	}
}
