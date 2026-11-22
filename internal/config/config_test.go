package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadCreatesDefaults(t *testing.T) {
	dir := t.TempDir()
	paths, err := ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != ConfigVersion {
		t.Fatalf("version = %d", cfg.Version)
	}
	if cfg.HTTP.Port != DefaultPort || cfg.HTTP.Token == "" {
		t.Fatalf("unexpected http defaults: %+v", cfg.HTTP)
	}
	if !cfg.HTTP.AllowLAN {
		t.Fatalf("LAN should be enabled by default")
	}
	if cfg.PDF.PageSize != "A4" || cfg.PDF.MarginMM != 20 {
		t.Fatalf("unexpected pdf defaults: %+v", cfg.PDF)
	}
	info, err := os.Stat(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o", info.Mode().Perm())
	}
	for _, dir := range []string{paths.Assets, paths.Templates, paths.Outputs, paths.Backups, paths.Logs, paths.Fonts} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Fatalf("missing dir %s: %v", dir, err)
		}
	}
}

func TestLoadIsStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	paths, _ := ResolvePaths(dir)
	first, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if first.HTTP.Token != second.HTTP.Token {
		t.Fatal("token changed between loads")
	}
}

func TestLoopbackConfigDisablesLAN(t *testing.T) {
	dir := t.TempDir()
	paths, _ := ResolvePaths(dir)
	if err := os.WriteFile(paths.ConfigFile, []byte(`{
	  "version": 1,
	  "http": {"bind": "0.0.0.0", "port": 9000, "allow_lan": false, "token": "x"},
	  "pdf": {"page_size": "A4", "margin_mm": 18},
	  "log": {"level": "debug", "max_size_mb": 5}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Bind != "127.0.0.1" {
		t.Fatalf("bind = %q", cfg.HTTP.Bind)
	}
	if cfg.ListenAddr() != "127.0.0.1:9000" {
		t.Fatalf("listen = %q", cfg.ListenAddr())
	}
}

func TestEnvFontOverride(t *testing.T) {
	dir := t.TempDir()
	paths, _ := ResolvePaths(dir)
	font := filepath.Join(dir, "my.ttf")
	if err := os.WriteFile(font, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvFont, font)
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PDF.FontPath != font {
		t.Fatalf("font = %q want %q", cfg.PDF.FontPath, font)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	paths, _ := ResolvePaths(dir)
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.Port = 8123
	cfg.Log.Level = "debug"
	if err := Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HTTP.Port != 8123 || loaded.Log.Level != "debug" {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}
