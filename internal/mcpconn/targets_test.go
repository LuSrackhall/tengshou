package mcpconn

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/config"
)

func TestIsMCPCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"mcp"}, true},
		{[]string{"mcp", "--data-dir", "/tmp/x"}, true},
		{[]string{"serve"}, false},
		{[]string{"MCP"}, false},
	}
	for _, c := range cases {
		if got := IsMCPCommand(c.args); got != c.want {
			t.Fatalf("IsMCPCommand(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

// An explicit address and token must bypass config.json entirely: the data
// directory does not even need to exist.
func TestTargetsExplicitURLSkipsConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	bases, token, err := targets(dir, "http://127.0.0.1:9999", "tok")
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
		t.Fatalf("bases = %v, want a single explicit address", bases)
	}
	if token != "tok" {
		t.Fatalf("token = %q, want %q", token, "tok")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("targets touched %s despite an explicit address", dir)
	}
}

// Without flags, candidates span the desktop app's port fallback range
// (configured port .. +9) and the token comes from config.json.
func TestTargetsFromConfig(t *testing.T) {
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

	bases, token, err := targets(dir, "", "")
	if err != nil {
		t.Fatalf("targets: %v", err)
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
func TestTargetsMissingAndCorruptConfig(t *testing.T) {
	t.Run("missing config with explicit address", func(t *testing.T) {
		bases, _, err := targets(t.TempDir(), "http://127.0.0.1:9999", "")
		if err != nil {
			t.Fatalf("targets: %v", err)
		}
		if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
			t.Fatalf("bases = %v, want a single explicit address", bases)
		}
	})

	t.Run("corrupt config with explicit address", func(t *testing.T) {
		dir := writeBrokenConfig(t)
		bases, _, err := targets(dir, "http://127.0.0.1:9999", "")
		if err != nil {
			t.Fatalf("explicit address should not need a readable config: %v", err)
		}
		if len(bases) != 1 || bases[0] != "http://127.0.0.1:9999" {
			t.Fatalf("bases = %v, want a single explicit address", bases)
		}
	})

	t.Run("corrupt config without address", func(t *testing.T) {
		dir := writeBrokenConfig(t)
		if _, _, err := targets(dir, "", ""); err == nil {
			t.Fatal("targets without an address should fail on a corrupt config")
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
