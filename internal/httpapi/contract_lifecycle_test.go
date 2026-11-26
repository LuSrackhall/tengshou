package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAppTerminateContract covers the v0.2 termination endpoint: an active
// contract ends early with a mandatory date and reason, releases the asset,
// and cannot be terminated twice.
func TestAppTerminateContract(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/assets", map[string]any{
		"name": "终止接口房产", "address": "测试路 1 号",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create asset: %d %s", resp.StatusCode, body)
	}
	assetID, _ := decodeAppItem(t, body)["id"].(string)
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"year": 2026, "title": "终止接口合同", "party_b_id": f.partyBID,
		"asset_id": assetID, "lease_start": "2026-01-01", "lease_end": "2026-12-31",
		"status": "active", "data": map[string]any{},
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create contract: %d %s", resp.StatusCode, body)
	}
	contractID, _ := decodeAppItem(t, body)["id"].(string)

	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contractID+"/terminate", map[string]any{
			"reason": "提前退租",
		}, nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_termination") {
		t.Fatalf("missing date: %d %s", resp.StatusCode, body)
	}

	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contractID+"/terminate", map[string]any{
			"terminated_on": "2026-06-30", "reason": "提前退租", "note": "工作调动",
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terminate: %d %s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	if item["status"] != "terminated" || item["terminated_on"] != "2026-06-30" ||
		item["termination_reason"] != "early" || item["termination_note"] != "工作调动" {
		t.Fatalf("terminated contract = %+v", item)
	}
	resp, body = f.get(t, "/app/api/v1/assets/"+assetID, false)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"lease_status":"vacant"`) {
		t.Fatalf("asset after termination: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contractID+"/terminate", map[string]any{
			"terminated_on": "2026-07-01", "reason": "作废",
		}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("repeat termination: %d %s", resp.StatusCode, body)
	}
}

// TestAppRenewalEarlyStartConfirmation covers the renewal linkage over HTTP:
// the earlier start is refused until the client confirms, then the source is
// terminated the day before the new lease starts.
func TestAppRenewalEarlyStartConfirmation(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/assets", map[string]any{
		"name": "续签接口房产",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create asset: %d %s", resp.StatusCode, body)
	}
	assetID, _ := decodeAppItem(t, body)["id"].(string)
	createSource := map[string]any{
		"year": 2026, "title": "接口原合同", "party_b_id": f.partyBID,
		"asset_id": assetID, "lease_start": "2026-01-01", "lease_end": "2026-12-31",
		"status": "active", "data": map[string]any{},
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", createSource, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create source: %d %s", resp.StatusCode, body)
	}
	source := decodeAppItem(t, body)
	sourceID, _ := source["id"].(string)

	renewal := map[string]any{
		"year": 2026, "title": "接口续签合同", "party_b_id": f.partyBID,
		"asset_id": assetID, "lease_start": "2026-06-01", "lease_end": "2027-05-31",
		"status": "active", "source_contract_id": sourceID, "data": map[string]any{},
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", renewal, nil)
	if resp.StatusCode != http.StatusConflict ||
		!strings.Contains(string(body), "renewal_termination_confirm") ||
		!strings.Contains(string(body), "2026-05-31") {
		t.Fatalf("early renewal: %d %s", resp.StatusCode, body)
	}
	// The refused save persisted nothing and left the source active.
	resp, body = f.get(t, "/app/api/v1/contracts/"+sourceID, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("source after refused renewal: %d %s", resp.StatusCode, body)
	}
	var detail struct {
		Contract map[string]any `json:"contract"`
	}
	if err := json.Unmarshal(body, &detail); err != nil || detail.Contract["status"] != "active" {
		t.Fatalf("source changed by refused renewal: %s", body)
	}

	renewal["renewal_termination_confirm"] = true
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", renewal, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirmed renewal: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/contracts/"+sourceID, false)
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Contract["status"] != "terminated" ||
		detail.Contract["termination_reason"] != "renewal" ||
		detail.Contract["terminated_on"] != "2026-05-31" {
		t.Fatalf("source after confirmed renewal: %s", body)
	}
}

// TestAppBootstrapExpiryCounters covers REQ-002 on the HTTP bootstrap: the
// desktop top bar counters count expiring/expired active leases and the
// reminder never changes the contract status.
func TestAppBootstrapExpiryCounters(t *testing.T) {
	f := newFixture(t)
	day := func(offset int) string {
		return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
	}
	create := func(title, start, end string) map[string]any {
		t.Helper()
		resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
			"year": time.Now().Year(), "title": title, "party_b_id": f.partyBID,
			"lease_start": start, "lease_end": end, "status": "active",
			"data": map[string]any{},
		}, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create %s: %d %s", title, resp.StatusCode, body)
		}
		return decodeAppItem(t, body)
	}
	expiring := create("接口即将到期合同", day(-300), day(10))
	create("接口已到期合同", day(-400), day(-1))
	create("接口远期合同", day(-1), day(100))

	resp, body := f.get(t, "/app/api/v1/bootstrap", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap: %d %s", resp.StatusCode, body)
	}
	var boot struct {
		Stats struct {
			ExpiringContracts int `json:"expiring_contracts"`
			ExpiredContracts  int `json:"expired_contracts"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &boot); err != nil {
		t.Fatalf("decode bootstrap: %v body=%s", err, body)
	}
	if boot.Stats.ExpiringContracts != 1 || boot.Stats.ExpiredContracts != 1 {
		t.Fatalf("bootstrap counters = %d/%d, want 1/1",
			boot.Stats.ExpiringContracts, boot.Stats.ExpiredContracts)
	}

	// The reminder is read-only: the contract is still active.
	resp, body = f.get(t, "/app/api/v1/contracts/"+expiring["id"].(string), false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expiring contract: %d %s", resp.StatusCode, body)
	}
	var detail struct {
		Contract map[string]any `json:"contract"`
	}
	if err := json.Unmarshal(body, &detail); err != nil || detail.Contract["status"] != "active" {
		t.Fatalf("reminder changed status: %s", body)
	}
}
