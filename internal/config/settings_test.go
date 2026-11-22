package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsMCPWriteTogglePersists(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MCP.WriteEnabled {
		t.Fatal("default write switch should be enabled")
	}
	settings := NewSettings(paths, cfg)
	if !settings.MCPWriteEnabled() {
		t.Fatal("settings should start enabled")
	}

	if err := settings.SetMCPWriteEnabled(false); err != nil {
		t.Fatal(err)
	}
	if settings.MCPWriteEnabled() {
		t.Fatal("in-memory switch did not flip")
	}
	reloaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MCP.WriteEnabled {
		t.Fatal("write switch was not persisted")
	}
	if CurrentMCPWriteEnabled(paths) {
		t.Fatal("read gate should see the persisted value")
	}

	// The config file keeps the mode that Save promises (0600 on POSIX).
	info, err := os.Stat(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config file mode = %v, want no group/other access", info.Mode().Perm())
	}

	if err := settings.SetMCPWriteEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !CurrentMCPWriteEnabled(paths) {
		t.Fatal("re-enable was not persisted")
	}
}

func TestCurrentMCPWriteEnabledDefaultsOnMissingFile(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !CurrentMCPWriteEnabled(paths) {
		t.Fatal("missing config should report the enabled default")
	}
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"mcp":{"write_enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if CurrentMCPWriteEnabled(paths) {
		t.Fatal("explicit false should be honored")
	}
	// Malformed JSON fails open at the connector; the HTTP guard still owns
	// enforcement.
	if err := os.WriteFile(paths.ConfigFile, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !CurrentMCPWriteEnabled(paths) {
		t.Fatal("malformed config should report the enabled default")
	}
}
