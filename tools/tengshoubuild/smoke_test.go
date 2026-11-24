package main

import (
	"strings"
	"testing"
)

// 真实发布形态的两条响应（截取断言需要的字段）。
const smokeInitOutput = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"tengshou","version":"v0.0.0"}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tengshou_status"},{"name":"tengshou_create_field"},{"name":"tengshou_list_contracts"}]}}
`

const smokeWriteOutput = `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"已创建字段 smoke_field（contract）"}],"isError":false}}
`

func TestParseMCPResponses(t *testing.T) {
	responses, err := parseMCPResponses([]byte(smokeInitOutput))
	if err != nil {
		t.Fatalf("parseMCPResponses: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("期望 2 条响应，实际 %d", len(responses))
	}
	if _, err := parseMCPResponses([]byte("not-json\n")); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	if responses, err := parseMCPResponses([]byte("\n \n")); err != nil || len(responses) != 0 {
		t.Fatalf("空输出应解析为 0 条：%v %d", err, len(responses))
	}
}

func TestCheckInitializeResponses(t *testing.T) {
	responses, err := parseMCPResponses([]byte(smokeInitOutput))
	if err != nil {
		t.Fatalf("parseMCPResponses: %v", err)
	}
	count, err := checkInitializeResponses(responses)
	if err != nil {
		t.Fatalf("checkInitializeResponses: %v", err)
	}
	if count != 3 {
		t.Fatalf("工具数应为 3，实际 %d", count)
	}
}

func TestCheckInitializeResponsesFailures(t *testing.T) {
	for name, out := range map[string]string{
		"响应条数不足": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}`,
		"服务名不符": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"other"}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tengshou_status"},{"name":"tengshou_create_field"}]}}`,
		"缺少写工具": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tengshou_status"}]}}`,
	} {
		responses, err := parseMCPResponses([]byte(out))
		if err != nil {
			t.Fatalf("%s: parseMCPResponses: %v", name, err)
		}
		if _, err := checkInitializeResponses(responses); err == nil {
			t.Fatalf("%s: 应报错", name)
		}
	}
}

func TestCheckWriteResponse(t *testing.T) {
	responses, err := parseMCPResponses([]byte(smokeWriteOutput))
	if err != nil {
		t.Fatalf("parseMCPResponses: %v", err)
	}
	if err := checkWriteResponse(responses); err != nil {
		t.Fatalf("checkWriteResponse: %v", err)
	}
}

func TestCheckWriteResponseFailures(t *testing.T) {
	for name, out := range map[string]string{
		"isError": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"smoke_field"}],"isError":true}}`,
		"缺少字段": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"其他内容"}],"isError":false}}`,
		"无内容": `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"tengshou"}}}
{"jsonrpc":"2.0","id":2,"result":{"isError":false}}`,
	} {
		responses, err := parseMCPResponses([]byte(out))
		if err != nil {
			t.Fatalf("%s: parseMCPResponses: %v", name, err)
		}
		if err := checkWriteResponse(responses); err == nil {
			t.Fatalf("%s: 应报错", name)
		}
	}
}

func TestReadLogTail(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/serve.log"
	if err := writeTestFile(path, "第一行\n第二行\n"); err != nil {
		t.Fatal(err)
	}
	if got := readLogTail(path); !strings.Contains(got, "第二行") {
		t.Fatalf("readLogTail 未读到内容：%q", got)
	}
	if got := readLogTail(dir + "/missing.log"); !strings.Contains(got, "读取日志失败") {
		t.Fatalf("缺失日志应给出提示：%q", got)
	}
}
