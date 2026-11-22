package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/LuSrackhall/tengshou/internal/service"
)

func (f *fixture) appRequest(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Tengshou-App", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func decodeAppItem(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		t.Fatalf("missing item: %s", body)
	}
	return item
}

func TestAppIndexAndAssets(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/app/", false)
	if resp.StatusCode != 200 {
		t.Fatalf("app index status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "合同台账") || !strings.Contains(string(body), "/app/app.js") {
		t.Fatalf("app index content = %s", body)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("app CSP = %q", csp)
	}
	resp, body = f.get(t, "/app/app.js", false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "use strict") {
		t.Fatalf("app js status=%d", resp.StatusCode)
	}
	resp, body = f.get(t, "/app/app.css", false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "--accent") {
		t.Fatalf("app css status=%d", resp.StatusCode)
	}
}

func TestAppAPIGuard(t *testing.T) {
	f := newFixture(t)
	post := func(remote, origin, appHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/app/api/v1/fields", strings.NewReader(`{"entity":"contract"}`))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if appHeader != "" {
			req.Header.Set("X-Tengshou-App", appHeader)
		}
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("127.0.0.1:5000", "", ""); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "app_header_required") {
		t.Fatalf("missing app header: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("192.168.1.20:5000", "", "1"); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "loopback_required") {
		t.Fatalf("lan access: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("127.0.0.1:5000", "https://evil.example", "1"); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "origin_rejected") {
		t.Fatalf("cross origin: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("127.0.0.1:5000", "http://127.0.0.1:5000", "1"); rec.Code == http.StatusForbidden {
		t.Fatalf("same origin rejected: %d %s", rec.Code, rec.Body.String())
	}
	// In-process desktop webview requests may have no RemoteAddr.
	if rec := post("", "", "1"); rec.Code == http.StatusForbidden {
		t.Fatalf("in-process request rejected: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("127.0.0.1:5000", "https://wails.localhost", "1"); rec.Code == http.StatusForbidden {
		t.Fatalf("wails origin rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAppFieldCRUD(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": "area_sqm", "label": "平方数", "type": "number",
		"required": false, "in_ledger": true, "in_template": true,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create field: %d %s", resp.StatusCode, body)
	}
	field := decodeAppItem(t, body)
	if field["key"] != "area_sqm" || field["built_in"] != false {
		t.Fatalf("field = %+v", field)
	}
	id, _ := field["id"].(string)
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/fields/"+id, map[string]any{
		"label": "建筑面积", "required": true, "in_ledger": true, "in_template": true,
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("update field: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/fields/"+id+"/archive", map[string]any{}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"archived":true`) {
		t.Fatalf("archive field: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/fields?entity=contract", true)
	if resp.StatusCode != 200 {
		t.Fatalf("list fields: %d %s", resp.StatusCode, body)
	}
	var listed map[string]any
	_ = json.Unmarshal(body, &listed)
	items, _ := listed["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["key"] == "area_sqm" {
			t.Fatal("archived field must not be listed active")
		}
	}
}

func TestAppContractLifecycle(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"year": 2026, "title": "应用接口合同", "party_b_id": f.partyID,
		"amount": 1200, "status": "active", "data": map[string]any{},
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("create contract: %d %s", resp.StatusCode, body)
	}
	contract := decodeAppItem(t, body)
	if contract["contract_no"] == "" || contract["year"] != float64(2026) {
		t.Fatalf("contract = %+v", contract)
	}
	id, _ := contract["id"].(string)
	number, _ := contract["contract_no"].(string)

	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/contracts/"+id, map[string]any{
		"contract_no": "CHANGED", "year": 2026, "title": "应用接口合同", "party_b_id": f.partyID,
	}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("immutable number should conflict: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/contracts/"+id, map[string]any{
		"contract_no": number, "year": 2026, "title": "改名后的合同", "party_b_id": f.partyID,
		"amount": 1500, "status": "active", "data": map[string]any{},
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("update contract: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/contracts?year=2026", false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "改名后的合同") {
		t.Fatalf("list contracts: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/contracts/years", false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"year":2026`) {
		t.Fatalf("years: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/changelog?entity=contract&entity_id="+id, false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "改名后的合同") {
		t.Fatalf("changelog: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/contracts/"+id, false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"party_b"`) {
		t.Fatalf("contract detail: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/contracts/"+id, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("delete contract: %d %s", resp.StatusCode, body)
	}
}

func TestAppPartyCRUDAndValidation(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "李四", "kind": "person", "phone": "13900000000",
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	party := decodeAppItem(t, body)
	id, _ := party["id"].(string)
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/parties/"+id, map[string]any{
		"name": "", "kind": "person",
	}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("empty name should be 422: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/parties/"+id, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("delete party: %d %s", resp.StatusCode, body)
	}
}

func TestAppExcelEndpoints(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/app/api/v1/excel/template", false)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("template: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if len(body) == 0 {
		t.Fatal("empty template")
	}
	resp, _ = f.get(t, "/app/api/v1/excel/ledger", false)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("ledger: %d", resp.StatusCode)
	}
	encoded := base64.StdEncoding.EncodeToString(body)
	resp, preview := f.appRequest(t, http.MethodPost, "/app/api/v1/excel/preview", map[string]any{
		"file_base64": encoded,
		"options":     map[string]any{},
	}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(preview), `"total":0`) {
		t.Fatalf("preview empty template: %d %s", resp.StatusCode, preview)
	}
	resp, result := f.appRequest(t, http.MethodPost, "/app/api/v1/excel/commit", map[string]any{
		"file_base64": encoded,
		"options":     map[string]any{},
	}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(result), `"created":0`) {
		t.Fatalf("commit empty template: %d %s", resp.StatusCode, result)
	}
	resp, _ = f.appRequest(t, http.MethodPost, "/app/api/v1/excel/preview", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing file should be 400: %d", resp.StatusCode)
	}
}

// B14 over HTTP: the exported ledger round-trips, conflicts are reported with
// both sides' values, and an unresolved conflict maps to 422.
func TestAppExcelRoundTripEndpoints(t *testing.T) {
	f := newFixture(t)
	resp, ledger := f.get(t, "/app/api/v1/excel/ledger", false)
	if resp.StatusCode != 200 || len(ledger) == 0 {
		t.Fatalf("ledger: %d", resp.StatusCode)
	}
	book, err := excelize.OpenReader(bytes.NewReader(ledger))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := book.GetRows("合同台账")
	if err != nil || len(rows) < 2 {
		t.Fatalf("ledger rows: %v", err)
	}
	amountCol := -1
	for i, header := range rows[0] {
		if strings.TrimSpace(header) == "金额（元）" {
			amountCol = i
			break
		}
	}
	if amountCol < 0 {
		t.Fatalf("ledger header = %v", rows[0])
	}
	cell, _ := excelize.CoordinatesToCellName(amountCol+1, 2)
	if err := book.SetCellValue("合同台账", cell, "4321"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := book.Write(&buf); err != nil {
		t.Fatal(err)
	}
	book.Close()
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	resp, preview := f.appRequest(t, http.MethodPost, "/app/api/v1/excel/preview", map[string]any{
		"file_base64": encoded,
		"options":     map[string]any{"file_name": "台账.xlsx"},
	}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(preview), `"action":"update"`) {
		t.Fatalf("preview: %d %s", resp.StatusCode, preview)
	}

	// Edit the same record in the system: both sides changed -> conflict.
	if _, _, err := f.service.SaveContract(context.Background(), service.ContractInput{
		ID: f.contractID, Title: "幸福村房屋租赁合同", PartyBID: f.partyBID, Amount: 9999,
	}); err != nil {
		t.Fatal(err)
	}
	resp, preview = f.appRequest(t, http.MethodPost, "/app/api/v1/excel/preview", map[string]any{
		"file_base64": encoded,
	}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(preview), `"action":"conflict"`) ||
		!strings.Contains(string(preview), `"system_values":{"amount":"9999"}`) ||
		!strings.Contains(string(preview), `"excel_values":{"amount":"4321"}`) {
		t.Fatalf("conflict preview: %d %s", resp.StatusCode, preview)
	}

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/excel/commit", map[string]any{
		"file_base64": encoded,
		"options":     map[string]any{"file_name": "台账.xlsx"},
	}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(string(body), "conflict_unresolved") {
		t.Fatalf("unresolved conflict commit: %d %s", resp.StatusCode, body)
	}

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/excel/commit", map[string]any{
		"file_base64": encoded,
		"options": map[string]any{
			"file_name":   "台账.xlsx",
			"resolutions": map[string]string{"2": "keep_system"},
		},
	}, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"conflicts":1`) ||
		!strings.Contains(string(body), `"skipped":1`) || !strings.Contains(string(body), `"updated":0`) {
		t.Fatalf("keep_system commit: %d %s", resp.StatusCode, body)
	}
	contract, err := f.reader.GetContract(context.Background(), f.contractID)
	if err != nil || contract.Amount != 9999 {
		t.Fatalf("keep_system contract = %+v err=%v", contract, err)
	}
}
