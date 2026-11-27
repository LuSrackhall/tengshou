package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Settings is a live, concurrency-safe view of the mutable application
// settings. Long-running components (the HTTP write guard and the MCP status
// panel) read switches through it, so flipping a switch takes effect on the
// very next request without restarting the process.
//
// The startup Config value is a snapshot; anything that must change at runtime
// goes through Settings.
type Settings struct {
	mu    sync.RWMutex
	paths Paths
	cfg   Config
}

// NewSettings wraps a configuration snapshot. paths is used to persist
// changes to config.json.
func NewSettings(paths Paths, cfg Config) *Settings {
	return &Settings{paths: paths, cfg: cfg}
}

// Snapshot returns the current configuration. The result is a copy.
func (s *Settings) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// MCPWriteEnabled reports whether MCP-originated writes are allowed.
func (s *Settings) MCPWriteEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.MCP.WriteEnabled
}

// SetMCPWriteEnabled flips the MCP write switch and persists config.json with
// the existing atomic 0600 writer. The in-memory value only changes when the
// file write succeeds, so a failed save never leaves the process advertising a
// setting that would revert on restart.
func (s *Settings) SetMCPWriteEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.MCP.WriteEnabled == enabled {
		return nil
	}
	next := s.cfg
	next.MCP.WriteEnabled = enabled
	if err := Save(s.paths, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

// Onboarding returns a copy of the first-run guide state (B8).
func (s *Settings) Onboarding() Onboarding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneOnboarding(s.cfg.Onboarding)
}

// SetOnboarding persists the first-run guide state with the atomic 0600
// writer. The in-memory value only changes when the file write succeeds, so a
// failed save never leaves the UI advertising a state that would revert.
func (s *Settings) SetOnboarding(next Onboarding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.cfg
	cfg.Onboarding = cloneOnboarding(next)
	if err := Save(s.paths, cfg); err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

func cloneOnboarding(in Onboarding) Onboarding {
	out := in
	if len(in.Steps) > 0 {
		out.Steps = make(map[string]string, len(in.Steps))
		for k, v := range in.Steps {
			out.Steps[k] = v
		}
	}
	return out
}

// ArchiveReuseNotify reports whether the contract form should show the
// archive-reuse notice (source: archive) and the fallback hint before applying
// archived signatures, signature stamps and seals. Reuse itself is always
// available; an absent key means the recommended default (notice shown so
// older config.json files keep working).
func (s *Settings) ArchiveReuseNotify() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.ArchiveReuse.Notify == nil {
		return true
	}
	return *s.cfg.ArchiveReuse.Notify
}

// SetArchiveReuseNotify persists the notice switch. The in-memory value only
// changes after the atomic config write succeeds.
func (s *Settings) SetArchiveReuseNotify(notify bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := notify
	if s.cfg.ArchiveReuse.Notify != nil && *s.cfg.ArchiveReuse.Notify == notify {
		return nil
	}
	next := s.cfg
	next.ArchiveReuse.Notify = &value
	if err := Save(s.paths, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

// EnrollEntryToken returns the long-lived self-enrollment token and whether it
// is currently disabled. An empty token means no fixed entry has been issued.
func (s *Settings) EnrollEntryToken() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.EnrollEntry.Token, s.cfg.EnrollEntry.Disabled
}

// RotateEnrollEntry issues a new long-lived enrollment token and re-enables the
// entry. Printed posters carrying the old token stop working immediately.
func (s *Settings) RotateEnrollEntry() (string, error) {
	token, err := newEnrollToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	next.EnrollEntry = EnrollEntry{Token: token}
	if err := Save(s.paths, next); err != nil {
		return "", err
	}
	s.cfg = next
	return token, nil
}

// DisableEnrollEntry makes the printed poster stop accepting submissions while
// keeping the token recorded. Rotating re-enables the entry with a new token.
func (s *Settings) DisableEnrollEntry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.EnrollEntry.Token == "" || s.cfg.EnrollEntry.Disabled {
		return nil
	}
	next := s.cfg
	next.EnrollEntry.Disabled = true
	if err := Save(s.paths, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func newEnrollToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成自助录入入口码: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// MCPWriteGate returns a function that reports the current mcp.write_enabled
// value from config.json. The MCP connector calls it before running a write
// tool, so a switch flipped in the desktop app applies to the next tool call
// without restarting the connector process. The running HTTP service remains
// the authoritative gate; unreadable configuration fails open here and closed
// there.
func MCPWriteGate(paths Paths) func() bool {
	return func() bool { return CurrentMCPWriteEnabled(paths) }
}

// CurrentMCPWriteEnabled reads mcp.write_enabled from config.json without
// creating or modifying the file. A missing file, malformed JSON or absent key
// reports the default (enabled).
func CurrentMCPWriteEnabled(paths Paths) bool {
	raw, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		return true
	}
	var file struct {
		MCP struct {
			WriteEnabled *bool `json:"write_enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.MCP.WriteEnabled == nil {
		return true
	}
	return *file.MCP.WriteEnabled
}
