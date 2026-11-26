package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAppAssetCRUD(t *testing.T) {
	f := newFixture(t)

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/assets", map[string]any{
		"name": "幸福里 3 号院 2 单元 501", "address": "幸福路 3 号",
		"room_no": "2-501", "area_sqm": 89.5, "note": "临近地铁",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d body=%s", resp.StatusCode, body)
	}
	created := decodeAppItem(t, body)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("missing id: %s", body)
	}
	if created["asset_type"] != "house" || created["enabled"] != true {
		t.Fatalf("created asset = %#v", created)
	}
	if created["lease_status"] != "vacant" {
		t.Fatalf("lease_status = %v, want vacant", created["lease_status"])
	}

	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/assets?status=enabled&q=%E5%B9%B8%E7%A6%8F", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.StatusCode, body)
	}
	var listed struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode list: %v body=%s", err, body)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0]["id"] != id {
		t.Fatalf("listed = %#v", listed)
	}

	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/assets/"+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d body=%s", resp.StatusCode, body)
	}
	var fetched map[string]any
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("decode get: %v body=%s", err, body)
	}
	if fetched["name"] != "幸福里 3 号院 2 单元 501" {
		t.Fatalf("get name = %v", fetched["name"])
	}

	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/assets/"+id, map[string]any{
		"name": "幸福里 3 号院 2 单元 501", "address": "幸福路 3 号（北门）", "enabled": false,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d body=%s", resp.StatusCode, body)
	}
	updated := decodeAppItem(t, body)
	if updated["enabled"] != false || updated["address"] != "幸福路 3 号（北门）" {
		t.Fatalf("updated asset = %#v", updated)
	}
	if _, ok := updated["area_sqm"]; ok {
		t.Fatalf("area_sqm should be cleared: %#v", updated)
	}

	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/assets?status=disabled", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list disabled status = %d body=%s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode disabled list: %v", err)
	}
	if listed.Total != 1 {
		t.Fatalf("disabled list total = %d body=%s", listed.Total, body)
	}

	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/assets/"+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", resp.StatusCode, body)
	}
	resp, _ = f.appRequest(t, http.MethodGet, "/app/api/v1/assets/"+id, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted status = %d", resp.StatusCode)
	}
}

func TestAppAssetValidationErrors(t *testing.T) {
	f := newFixture(t)

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/assets", map[string]any{"name": " "}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid name status = %d body=%s", resp.StatusCode, body)
	}
	var payload struct {
		Error struct {
			Code   string `json:"code"`
			Fields []struct {
				Field string `json:"field"`
			} `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if payload.Error.Code != "validation" || len(payload.Error.Fields) != 1 ||
		payload.Error.Fields[0].Field != "name" {
		t.Fatalf("validation payload = %s", body)
	}

	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/assets?status=archived", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", resp.StatusCode, body)
	}
}
