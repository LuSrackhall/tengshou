package mcpconn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/app"
	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/httpapi"
)

// TestRunServesSharedProtocol drives Run exactly the way both entry points do:
// a real local HTTP service behind it (with its middleware, validation and
// change log) and newline-delimited JSON-RPC over stdio. The desktop binary and
// the CLI both delegate to Run, so this single test locks the behavior they
// share: initialize, tools/list, a read tool and a write tool.
func TestRunServesSharedProtocol(t *testing.T) {
	dir := t.TempDir()
	a, err := app.Open(dir)
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	cfg := a.Config
	cfg.HTTP.Bind = "127.0.0.1"
	cfg.HTTP.Port = 0 // take an ephemeral port; tests never bind a fixed one
	cfg.HTTP.AllowLAN = false
	cfg.MCP.WriteEnabled = true
	server := httpapi.New(httpapi.Options{
		Reader:   a.Reader,
		Service:  a.Service,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:   cfg,
		Settings: config.NewSettings(a.Paths, cfg),
		Version:  "test",
	})
	ln, err := server.Listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, ln) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr = %T, want *net.TCPAddr", ln.Addr())
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", tcp.Port)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tengshou_list_parties","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"tengshou_create_party","arguments":{"name":"共享实现相对方","kind":"person"}}}`,
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	args := []string{"--data-dir", dir, "--api-url", baseURL, "--token", a.Config.HTTP.Token}
	if err := Run(args, strings.NewReader(input), &stdout, &stderr); err != nil {
		t.Fatalf("Run: %v（stderr：%s）", err, stderr.String())
	}

	responses := map[int]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("stdout 不是 JSON-RPC 消息：%v（%q）", err, line)
		}
		id, ok := msg["id"].(float64)
		if !ok {
			continue
		}
		responses[int(id)] = msg
	}

	resultOf := func(id int) map[string]any {
		t.Helper()
		msg, ok := responses[id]
		if !ok {
			t.Fatalf("缺少 id=%d 的响应；stdout=%q", id, stdout.String())
		}
		if rpcErr, ok := msg["error"]; ok {
			t.Fatalf("id=%d 返回 JSON-RPC 错误：%v", id, rpcErr)
		}
		result, ok := msg["result"].(map[string]any)
		if !ok {
			t.Fatalf("id=%d 缺少 result 对象：%v", id, msg)
		}
		return result
	}

	init := resultOf(1)
	serverInfo, ok := init["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize 缺少 serverInfo：%v", init)
	}
	if name, _ := serverInfo["name"].(string); name == "" {
		t.Fatalf("initialize 缺少 serverInfo.name：%v", serverInfo)
	}

	tools, ok := resultOf(2)["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list 为空或格式不符：%v", resultOf(2))
	}
	names := map[string]bool{}
	for _, raw := range tools {
		if tool, ok := raw.(map[string]any); ok {
			if name, ok := tool["name"].(string); ok {
				names[name] = true
			}
		}
	}
	for _, want := range []string{"tengshou_status", "tengshou_create_party"} {
		if !names[want] {
			t.Fatalf("tools/list 缺少 %s：%v", want, names)
		}
	}

	assertItemText := func(id int, wantName string) {
		t.Helper()
		result := resultOf(id)
		if isErr, _ := result["isError"].(bool); isErr {
			t.Fatalf("id=%d 工具调用失败：%v", id, result)
		}
		content, ok := result["content"].([]any)
		if !ok || len(content) == 0 {
			t.Fatalf("id=%d 缺少 content：%v", id, result)
		}
		first, _ := content[0].(map[string]any)
		text, _ := first["text"].(string)
		var payload struct {
			Item struct {
				Name string `json:"name"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("id=%d 工具返回不是 JSON：%v（%q）", id, err, text)
		}
		if payload.Item.Name != wantName {
			t.Fatalf("id=%d item.name = %q, want %q", id, payload.Item.Name, wantName)
		}
	}
	// The read tool must work even before any data exists.
	if result := resultOf(3); result["isError"] == true {
		t.Fatalf("tengshou_list_parties 失败：%v", result)
	}
	// The write tool goes through the service validation and change log.
	assertItemText(4, "共享实现相对方")

	if logs := stderr.String(); !strings.Contains(logs, "MCP 服务已启动") {
		t.Fatalf("stderr 未见启动日志：%q", logs)
	}
}
