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

func TestArchiveReuseNotifyDefaultsOnAndPersistsOff(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	settings := NewSettings(paths, cfg)
	if !settings.ArchiveReuseNotify() {
		t.Fatal("archive reuse notice should default to shown")
	}
	if err := settings.SetArchiveReuseNotify(false); err != nil {
		t.Fatal(err)
	}
	if settings.ArchiveReuseNotify() {
		t.Fatal("in-memory notice switch did not flip")
	}
	reloaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if NewSettings(paths, reloaded).ArchiveReuseNotify() {
		t.Fatal("explicit false was not persisted")
	}
	if err := settings.SetArchiveReuseNotify(true); err != nil {
		t.Fatal(err)
	}
	reloaded, err = Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !NewSettings(paths, reloaded).ArchiveReuseNotify() {
		t.Fatal("re-enable was not persisted")
	}
}

func TestEnrollEntryRotateDisablePersists(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	settings := NewSettings(paths, cfg)
	token, disabled := settings.EnrollEntryToken()
	if token != "" || disabled {
		t.Fatalf("fresh entry should be unissued, got token=%q disabled=%v", token, disabled)
	}
	first, err := settings.RotateEnrollEntry()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("token length = %d, want 64", len(first))
	}
	if err := settings.DisableEnrollEntry(); err != nil {
		t.Fatal(err)
	}
	if token, disabled := settings.EnrollEntryToken(); token != first || !disabled {
		t.Fatalf("disable lost token or state: %q disabled=%v", token, disabled)
	}
	reloaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	persisted := NewSettings(paths, reloaded)
	if token, disabled := persisted.EnrollEntryToken(); token != first || !disabled {
		t.Fatalf("disable was not persisted: %q disabled=%v", token, disabled)
	}
	second, err := persisted.RotateEnrollEntry()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("rotate should issue a new token")
	}
	if token, disabled := persisted.EnrollEntryToken(); token != second || disabled {
		t.Fatalf("rotate should re-enable with the new token: %q disabled=%v", token, disabled)
	}
}
