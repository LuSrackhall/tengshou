package config

import (
	"os"
	"runtime"
	"testing"
)

// TestSettingsOnboardingPersists covers the B8 guide bookkeeping: completing,
// skipping and per-step timestamps live in config.json and never touch the
// database.
func TestSettingsOnboardingPersists(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	settings := NewSettings(paths, cfg)
	if state := settings.Onboarding(); state.Completed || state.Skipped || len(state.Steps) != 0 {
		t.Fatalf("fresh onboarding state = %+v", state)
	}

	next := settings.Onboarding()
	next.Completed = true
	next.CompletedAt = "2026-11-23T01:02:03Z"
	next.Steps = map[string]string{"sample_preview": "2026-11-23T01:00:00Z"}
	if err := settings.SetOnboarding(next); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Onboarding.Completed || reloaded.Onboarding.CompletedAt != next.CompletedAt {
		t.Fatalf("completed state not persisted: %+v", reloaded.Onboarding)
	}
	if reloaded.Onboarding.Steps["sample_preview"] != "2026-11-23T01:00:00Z" {
		t.Fatalf("step progress not persisted: %+v", reloaded.Onboarding.Steps)
	}

	// The snapshot is a deep copy: callers cannot mutate live state.
	got := settings.Onboarding()
	got.Steps["trial_pdf"] = "2026-11-23T01:05:00Z"
	if _, exists := settings.Onboarding().Steps["trial_pdf"]; exists {
		t.Fatal("Onboarding() returned a shared steps map")
	}

	skipped := settings.Onboarding()
	skipped.Completed = false
	skipped.CompletedAt = ""
	skipped.Skipped = true
	skipped.SkippedAt = "2026-11-23T02:00:00Z"
	if err := settings.SetOnboarding(skipped); err != nil {
		t.Fatal(err)
	}
	reloaded, err = Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Onboarding.Skipped || reloaded.Onboarding.SkippedAt == "" || reloaded.Onboarding.Completed {
		t.Fatalf("skipped state not persisted: %+v", reloaded.Onboarding)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("config file mode = %v, want no group/other access", info.Mode().Perm())
		}
	}
}

// TestOnboardingDefaultsOnMissingSection keeps older config files portable:
// a config.json without an onboarding object loads as a fresh guide state.
func TestOnboardingDefaultsOnMissingSection(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{
	  "version": 1,
	  "http": {"port": 9001, "allow_lan": false, "token": "x"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Onboarding.Completed || cfg.Onboarding.Skipped {
		t.Fatalf("missing onboarding section should be empty: %+v", cfg.Onboarding)
	}
}
