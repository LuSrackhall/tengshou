package main

import (
	"strings"
	"testing"
)

func TestRunRejectsMissingAndUnknownSubcommand(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "缺少子命令") {
		t.Fatalf("run(nil) error = %v, want 缺少子命令", err)
	}
	if err := run([]string{"unknown-subcommand"}); err == nil {
		t.Fatal("run(unknown-subcommand) should return an error")
	} else if !strings.Contains(err.Error(), "未知子命令") {
		t.Fatalf("run(unknown-subcommand) error = %v, want 未知子命令", err)
	}
}
