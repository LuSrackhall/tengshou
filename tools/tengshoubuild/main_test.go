package main

import (
	"strings"
	"testing"
)

func TestRunRejectsMissingSubcommand(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "缺少子命令") {
		t.Fatalf("缺少子命令应报错，实际：%v", err)
	}
}

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	if err := run([]string{"deploy"}); err == nil || !strings.Contains(err.Error(), "未知子命令") {
		t.Fatalf("未知子命令应报错，实际：%v", err)
	}
}

func TestRunHelp(t *testing.T) {
	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("--help 应成功：%v", err)
	}
}

func TestSmokeMCPUsage(t *testing.T) {
	if err := run([]string{"smoke-mcp"}); err == nil || !strings.Contains(err.Error(), "用法") {
		t.Fatalf("参数不足应给出用法，实际：%v", err)
	}
	if err := run([]string{"smoke-mcp", "a"}); err == nil || !strings.Contains(err.Error(), "用法") {
		t.Fatalf("参数不足应给出用法，实际：%v", err)
	}
}

func TestPackageWindowsZipUsage(t *testing.T) {
	if err := run([]string{"package-windows-zip", "a", "b"}); err == nil || !strings.Contains(err.Error(), "用法") {
		t.Fatalf("多余参数应给出用法，实际：%v", err)
	}
}
