package httpapi_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPStatusExposesRegistrationCommand pins the in-app MCP access contract:
// the status payload names the running executable (the desktop exe on Windows
// and macOS) and the unified `<exe> mcp --data-dir "<dir>"` registration
// command that the settings panel renders and copies.
func TestMCPStatusExposesRegistrationCommand(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/app/api/v1/mcp/status", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp status: %d %s", resp.StatusCode, body)
	}
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}

	execPath, _ := status["exec_path"].(string)
	if !filepath.IsAbs(execPath) {
		t.Fatalf("exec_path 必须是绝对路径，实际 %q", execPath)
	}
	command, _ := status["command"].(string)
	quoted := execPath
	if strings.ContainsAny(execPath, " \t") {
		quoted = `"` + execPath + `"`
	}
	want := quoted + ` mcp --data-dir "` + f.dataDir + `"`
	if command != want {
		t.Fatalf("command = %q，期望 %q", command, want)
	}
	if strings.Contains(command, "tengshou-cli") {
		t.Fatalf("command 不应再写死 CLI 名称：%q", command)
	}
	if dataDir, _ := status["data_dir"].(string); dataDir != f.dataDir {
		t.Fatalf("data_dir = %q，期望 %q", dataDir, f.dataDir)
	}
	if transport, _ := status["transport"].(string); transport != "stdio" {
		t.Fatalf("transport = %v，期望 stdio", status["transport"])
	}
}
