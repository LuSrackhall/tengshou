package httpapi

import "testing"

// TestQuoteCommandArg 用固定字面期望覆盖引用分支：含空格/制表符加双引号，
// 无空格保持原样。HTTP 集成测试的运行路径不含空格，无法触达这些分支。
func TestQuoteCommandArg(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Program Files\Tengshou\tengshou.exe`, `"C:\Program Files\Tengshou\tengshou.exe"`},
		{`C:\Tengshou\tengshou.exe`, `C:\Tengshou\tengshou.exe`},
		{"a\tb", "\"a\tb\""},
		{``, ``},
	}
	for _, c := range cases {
		if got := quoteCommandArg(c.in); got != c.want {
			t.Errorf("quoteCommandArg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMCPAccessCommand 锁定统一接入命令的固定期望形态：exe 为空时退化为
// `tengshou mcp`，数据目录为空时省略 `--data-dir`。
func TestMCPAccessCommand(t *testing.T) {
	cases := []struct{ exe, dir, want string }{
		{`C:\Program Files\Tengshou\tengshou.exe`, `C:\Users\张三\AppData\Local\Tengshou`,
			`"C:\Program Files\Tengshou\tengshou.exe" mcp --data-dir "C:\Users\张三\AppData\Local\Tengshou"`},
		{`/opt/tengshou`, `/data`, `/opt/tengshou mcp --data-dir "/data"`},
		{``, `/data`, `tengshou mcp --data-dir "/data"`},
		{``, ``, `tengshou mcp`},
	}
	for _, c := range cases {
		if got := mcpAccessCommand(c.exe, c.dir); got != c.want {
			t.Errorf("mcpAccessCommand(%q, %q) = %q, want %q", c.exe, c.dir, got, c.want)
		}
	}
}
