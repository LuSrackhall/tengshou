package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestVersionVisibleAcrossSurfaces pins the user-facing version surfaces:
// bootstrap (desktop UI header/settings), the MCP status panel payload, and
// the read-only API root/schema. MCP initialize carries the same version in
// serverInfo (covered by internal/mcp tests).
func TestVersionVisibleAcrossSurfaces(t *testing.T) {
	f := newFixture(t)
	decode := func(body []byte) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		return m
	}

	resp, body := f.get(t, "/app/api/v1/bootstrap", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap: %d %s", resp.StatusCode, body)
	}
	if got := decode(body)["version"]; got != "test" {
		t.Fatalf("bootstrap version = %v, want test", got)
	}

	resp, body = f.get(t, "/app/api/v1/mcp/status", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp status: %d %s", resp.StatusCode, body)
	}
	if got := decode(body)["version"]; got != "test" {
		t.Fatalf("mcp status version = %v, want test", got)
	}

	resp, body = f.get(t, "/api/v1", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api root: %d %s", resp.StatusCode, body)
	}
	root := decode(body)
	if root["app_version"] != "test" || root["version"] != "v1" {
		t.Fatalf("api root versions = %v", root)
	}

	resp, body = f.get(t, "/api/v1/schema", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("schema: %d %s", resp.StatusCode, body)
	}
	if got := decode(body)["app_version"]; got != "test" {
		t.Fatalf("schema app_version = %v, want test", got)
	}
}
