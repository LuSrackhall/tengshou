package mcpconn

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// The shared entry point must serve initialize / tools/list with no HTTP
// service running: that is what both the desktop exe and the CLI expose to an
// agent before the app is started. The assertions here lock the shared wiring
// (flag parsing, stdio serving) so the desktop binary cannot drift from the
// tested CLI path.
func TestRunServesInitializeAndToolsList(t *testing.T) {
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	err := Run([]string{"--data-dir", t.TempDir()}, strings.NewReader(requests), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run: %v\nstderr: %s", err, stderr.String())
	}

	lines := nonEmptyLines(stdout.String())
	if len(lines) != 2 {
		t.Fatalf("stdout has %d JSON-RPC responses, want 2 (notifications get none):\n%s",
			len(lines), stdout.String())
	}

	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatalf("initialize response is not JSON: %v\n%s", err, lines[0])
	}
	if init.Result.ServerInfo.Name != "tengshou" {
		t.Fatalf("serverInfo.name = %q, want tengshou", init.Result.ServerInfo.Name)
	}
	if init.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("protocolVersion = %q, want 2025-06-18", init.Result.ProtocolVersion)
	}

	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatalf("tools/list response is not JSON: %v\n%s", err, lines[1])
	}
	names := make(map[string]bool, len(listed.Result.Tools))
	for _, tool := range listed.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"tengshou_status", "tengshou_create_field"} {
		if !names[want] {
			t.Fatalf("tools/list missing %q; got %v", want, names)
		}
	}

	if !strings.Contains(stderr.String(), "腾手 MCP 服务已启动（stdio）") {
		t.Fatalf("startup log should go to stderr, got: %q", stderr.String())
	}
}

// The shared entry must reject non-loopback --api-url before serving, so the
// desktop binary inherits the same security boundary as the CLI.
func TestRunRejectsNonLoopbackAPIURL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(
		[]string{"--data-dir", t.TempDir(), "--api-url", "http://10.1.2.3:8765", "--token", "tok"},
		strings.NewReader(""), &stdout, &stderr,
	)
	if err == nil {
		t.Fatalf("Run accepted a non-loopback address; stderr: %s", stderr.String())
	}
	if !strings.Contains(err.Error(), "回环") {
		t.Fatalf("error should explain the loopback boundary, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay protocol-only, got: %q", stdout.String())
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
