// Package config resolves the on-disk data layout and application settings.
//
// 腾手 is a single-user, offline-first desktop application. All state lives in
// one data directory:
//
//	<data>/
//	  config.json      application settings (contains the local API token)
//	  tengshou.db      SQLite database
//	  assets/          binary assets referenced by templates (signatures, seals)
//	  templates/       sample HTML templates (demo seed, read-only)
//	  outputs/         generated PDFs
//	  backups/         database backups
//	  logs/            application logs
//
// The directory is chosen from --data-dir / TENGHSHOU_DATA_DIR, falling back
// to the platform application-data directory.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	// EnvDataDir overrides the data directory.
	EnvDataDir = "TENGHSHOU_DATA_DIR"
	// EnvFont overrides the PDF font file.
	EnvFont = "TENGHSHOU_FONT"

	// DefaultPort is the local HTTP service port.
	DefaultPort = 8765
	// PortFallbackSpan is how many consecutive ports (configured port .. +9)
	// the desktop service tries to bind and the MCP connector probes. Both
	// sides must use this one constant, or a running service is never found.
	PortFallbackSpan = 10
	// ConfigVersion is the current config file schema version.
	ConfigVersion = 1
)

// Paths is the resolved data directory layout.
type Paths struct {
	Root       string
	DB         string
	ConfigFile string
	Assets     string
	Templates  string
	Outputs    string
	Backups    string
	Logs       string
	Fonts      string
}

// HTTP controls the embedded local HTTP service.
type HTTP struct {
	// Bind is the listen address; 0.0.0.0 exposes the test page to the LAN.
	Bind string `json:"bind"`
	// Port is the listen port.
	Port int `json:"port"`
	// AllowLAN binds all interfaces. Phone devices on the same LAN can reach
	// the public test page; data endpoints still require Token.
	AllowLAN bool `json:"allow_lan"`
	// Token protects /api/v1 data endpoints. It is generated on first run.
	Token string `json:"token"`
}

// PDF controls document generation.
type PDF struct {
	// FontPath is an explicit CJK-capable font. Empty means auto-detect.
	FontPath string `json:"font_path"`
	// FontIndex selects a face inside a .ttc collection.
	FontIndex int `json:"font_index"`
	// PageSize is the paper size; only A4 is supported in v0.1.
	PageSize string `json:"page_size"`
	// MarginMM is the page margin in millimetres.
	MarginMM float64 `json:"margin_mm"`
}

// Log controls file logging.
type Log struct {
	Level string `json:"level"` // debug, info, warn, error
	// MaxSizeMB rotates the log file once it grows past this size.
	MaxSizeMB int `json:"max_size_mb"`
}

// Signature controls the LAN QR-code signature capture sessions.
type Signature struct {
	// TTLMinutes is how long a signing QR code stays valid.
	TTLMinutes int `json:"ttl_minutes"`
}

// MCP controls the local B6 MCP connector (the `tengshou-cli mcp` stdio
// subcommand and the loopback write surface it uses).
type MCP struct {
	// WriteEnabled allows the MCP connector's write tools (create field,
	// party and contract draft). It is on by default; set it to false to keep
	// MCP strictly read-only. Read tools are unaffected.
	WriteEnabled bool `json:"write_enabled"`
}

// Onboarding records first-run guide state (B8). It is UI bookkeeping only:
// completing, skipping or reopening the guide never writes business data.
// First-run detection itself is computed from the data directory (no template
// and no contract), so an existing vault copied to a new machine is not
// mistaken for a fresh install.
type Onboarding struct {
	Completed   bool   `json:"completed"`
	Skipped     bool   `json:"skipped"`
	CompletedAt string `json:"completed_at,omitempty"`
	SkippedAt   string `json:"skipped_at,omitempty"`
	// Steps records the wizard steps that leave no business data behind
	// (sample preview and trial PDF generation). Data-backed steps are derived
	// from the database on every read.
	Steps map[string]string `json:"steps,omitempty"`
}

// Config is the application configuration persisted as config.json.
type Config struct {
	Version    int        `json:"version"`
	HTTP       HTTP       `json:"http"`
	PDF        PDF        `json:"pdf"`
	Log        Log        `json:"log"`
	Signature  Signature  `json:"signature"`
	MCP        MCP        `json:"mcp"`
	Onboarding Onboarding `json:"onboarding"`
	// Operator is the name recorded in the change log. Empty means the OS user.
	Operator string `json:"operator,omitempty"`
}

// ResolvePaths returns the data directory layout for dir. An empty dir means
// TENGHSHOU_DATA_DIR, then the platform default.
func ResolvePaths(dir string) (Paths, error) {
	if dir == "" {
		dir = os.Getenv(EnvDataDir)
	}
	if dir == "" {
		var err error
		dir, err = defaultDataDir()
		if err != nil {
			return Paths{}, err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data dir: %w", err)
	}
	return Paths{
		Root:       abs,
		DB:         filepath.Join(abs, "tengshou.db"),
		ConfigFile: filepath.Join(abs, "config.json"),
		Assets:     filepath.Join(abs, "assets"),
		Templates:  filepath.Join(abs, "templates"),
		Outputs:    filepath.Join(abs, "outputs"),
		Backups:    filepath.Join(abs, "backups"),
		Logs:       filepath.Join(abs, "logs"),
		Fonts:      filepath.Join(abs, "fonts"),
	}, nil
}

func defaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "Tengshou"), nil
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Tengshou"), nil
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Tengshou"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "tengshou"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "tengshou"), nil
	}
	return "", errors.New("cannot determine the platform data directory")
}

// EnsureDirs creates the data directory layout with user-only permissions.
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.Root, p.Assets, p.Templates, p.Outputs, p.Backups, p.Logs, p.Fonts} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

// HardenPermissions applies least-privilege permissions to the data directory:
// directories 0700 and regular files 0600. Signature and seal images are
// sensitive assets, so assets/ and templates/ are walked on startup. On
// Windows the POSIX bits are largely advisory; ACLs still decide access.
func (p Paths) HardenPermissions() error {
	dirs := []string{p.Root, p.Assets, p.Templates, p.Outputs, p.Backups, p.Logs, p.Fonts}
	for _, dir := range dirs {
		if err := os.Chmod(dir, 0o700); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("tighten %s: %w", dir, err)
		}
	}
	for _, file := range []string{p.ConfigFile, p.DB} {
		if err := os.Chmod(file, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("tighten %s: %w", file, err)
		}
	}
	for _, dir := range []string{p.Assets, p.Templates} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return os.Chmod(path, 0o700)
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				return os.Chmod(path, 0o600)
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("tighten %s: %w", dir, err)
		}
	}
	return nil
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Version: ConfigVersion,
		HTTP: HTTP{
			Bind:     "0.0.0.0",
			Port:     DefaultPort,
			AllowLAN: true,
		},
		PDF: PDF{
			PageSize: "A4",
			MarginMM: 20,
		},
		Log: Log{
			Level:     "info",
			MaxSizeMB: 10,
		},
		Signature: Signature{
			TTLMinutes: 10,
		},
		MCP: MCP{
			WriteEnabled: true,
		},
	}
}

// Load reads config.json, creating it with safe defaults on first run.
func Load(paths Paths) (Config, error) {
	if err := paths.EnsureDirs(); err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, fs.ErrNotExist) {
		cfg := Default()
		if err := cfg.normalize(paths); err != nil {
			return Config{}, err
		}
		if err := Save(paths, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", paths.ConfigFile, err)
	}
	if err := cfg.normalize(paths); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize(paths Paths) error {
	if c.Version == 0 {
		c.Version = ConfigVersion
	}
	if c.Version > ConfigVersion {
		return fmt.Errorf("config version %d is newer than supported version %d", c.Version, ConfigVersion)
	}
	if c.HTTP.Port == 0 {
		c.HTTP.Port = DefaultPort
	}
	if c.HTTP.Port < 0 || c.HTTP.Port > 65535 {
		return fmt.Errorf("invalid http port %d", c.HTTP.Port)
	}
	if c.HTTP.Bind == "" {
		if c.HTTP.AllowLAN {
			c.HTTP.Bind = "0.0.0.0"
		} else {
			c.HTTP.Bind = "127.0.0.1"
		}
	}
	if c.HTTP.AllowLAN && c.HTTP.Bind == "127.0.0.1" {
		c.HTTP.Bind = "0.0.0.0"
	}
	if !c.HTTP.AllowLAN && (c.HTTP.Bind == "0.0.0.0" || c.HTTP.Bind == "::") {
		c.HTTP.Bind = "127.0.0.1"
	}
	if c.HTTP.AllowLAN && c.HTTP.Token == "" {
		token, err := NewToken()
		if err != nil {
			return err
		}
		c.HTTP.Token = token
	}
	if c.PDF.PageSize == "" {
		c.PDF.PageSize = "A4"
	}
	if !strings.EqualFold(c.PDF.PageSize, "A4") {
		return fmt.Errorf("unsupported pdf page size %q (only A4)", c.PDF.PageSize)
	}
	c.PDF.PageSize = "A4"
	if c.PDF.MarginMM <= 0 || c.PDF.MarginMM > 60 {
		c.PDF.MarginMM = 20
	}
	if env := os.Getenv(EnvFont); env != "" {
		c.PDF.FontPath = env
	}
	if c.PDF.FontPath != "" {
		abs, err := filepath.Abs(c.PDF.FontPath)
		if err != nil {
			return fmt.Errorf("resolve font path: %w", err)
		}
		c.PDF.FontPath = abs
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
		c.Log.Level = strings.ToLower(c.Log.Level)
	default:
		c.Log.Level = "info"
	}
	if c.Log.MaxSizeMB <= 0 {
		c.Log.MaxSizeMB = 10
	}
	if c.Signature.TTLMinutes <= 0 || c.Signature.TTLMinutes > 120 {
		c.Signature.TTLMinutes = 10
	}
	return nil
}

// Save writes config.json atomically with 0600 permissions.
func Save(paths Paths, cfg Config) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(paths.Root, ".config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, paths.ConfigFile)
}

// NewToken returns a cryptographically random API token.
func NewToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate api token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ListenAddr returns the host:port the HTTP service should bind.
func (c Config) ListenAddr() string {
	host := c.HTTP.Bind
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(c.HTTP.Port))
}
