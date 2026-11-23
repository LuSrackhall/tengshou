package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/config"
)

// An explicit address and token must bypass config.json entirely: the data
// directory does not even need to exist.
func TestMCPTargetsExplicitURLSkipsConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	bases, token, err := mcpTargets(dir, "http://127.0.0.1:9999", "tok")
	if err != nil {
		t.Fatalf("mcpTargets: %v", err)
	}
	if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
		t.Fatalf("bases = %v, want a single explicit address", bases)
	}
	if token != "tok" {
		t.Fatalf("token = %q, want %q", token, "tok")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("mcpTargets touched %s despite an explicit address", dir)
	}
}

// Without flags, candidates span the desktop app's port fallback range
// (configured port .. +9) and the token comes from config.json.
func TestMCPTargetsFromConfig(t *testing.T) {
	dir := t.TempDir()
	paths, err := config.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HTTP.Port = 8790
	cfg.HTTP.Token = "config-token"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}

	bases, token, err := mcpTargets(dir, "", "")
	if err != nil {
		t.Fatalf("mcpTargets: %v", err)
	}
	if len(bases) != 10 {
		t.Fatalf("len(bases) = %d, want 10: %v", len(bases), bases)
	}
	for i, base := range bases {
		want := fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTP.Port+i)
		if base != want {
			t.Fatalf("bases[%d] = %q, want %q", i, base, want)
		}
	}
	if token != "config-token" {
		t.Fatalf("token = %q, want config token", token)
	}
}

// A missing config falls back to defaults (Load creates it); a corrupt one is
// an error unless the user passed an explicit --api-url.
func TestMCPTargetsMissingAndCorruptConfig(t *testing.T) {
	t.Run("missing config with explicit address", func(t *testing.T) {
		bases, _, err := mcpTargets(t.TempDir(), "http://127.0.0.1:9999", "")
		if err != nil {
			t.Fatalf("mcpTargets: %v", err)
		}
		if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
			t.Fatalf("bases = %v, want a single explicit address", bases)
		}
	})

	t.Run("corrupt config with explicit address", func(t *testing.T) {
		dir := writeBrokenConfig(t)
		bases, _, err := mcpTargets(dir, "http://127.0.0.1:9999", "")
		if err != nil {
			t.Fatalf("explicit address should not need a readable config: %v", err)
		}
		if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
			t.Fatalf("bases = %v, want a single explicit address", bases)
		}
	})

	t.Run("corrupt config without address", func(t *testing.T) {
		dir := writeBrokenConfig(t)
		if _, _, err := mcpTargets(dir, "", ""); err == nil {
			t.Fatal("mcpTargets without an address should fail on a corrupt config")
		}
	})
}

func writeBrokenConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	paths, err := config.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

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
