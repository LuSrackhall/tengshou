// Package backup implements the B9 full-data backup package: a consistent
// SQLite snapshot plus every user asset directory and the configuration file,
// with a manifest recording application/schema versions and SHA-256 digests.
//
// The package layout is deliberately generic: the database is copied as one
// opaque file (VACUUM INTO), and assets are packaged by walking whole
// directories. New tables, columns or asset subdirectories (for example B3's
// templates/pdf/<id>/source.docx) are included without code changes.
package backup

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
)

const (
	// FormatName identifies 腾手 backup packages.
	FormatName = "tengshou-backup"
	// FormatVersion is the package layout version understood by this code.
	FormatVersion = 1

	// ManifestName is the manifest entry at the package root.
	ManifestName = "manifest.json"
	// DatabaseEntry is the package path of the SQLite snapshot. It describes
	// the package layout; restores map it onto <data>/tengshou.db.
	DatabaseEntry = "db/tengshou.db"
	// ConfigEntry is the package path of config.json.
	ConfigEntry = "config.json"

	// DefaultMaxUploadBytes bounds a restore upload read from the network.
	DefaultMaxUploadBytes int64 = 8 << 30 // 8 GiB
	// MaxEntries bounds the number of zip entries (zip-bomb guard).
	MaxEntries = 100000
	// MaxTotalUncompressed bounds the sum of declared sizes (zip-bomb guard).
	MaxTotalUncompressed int64 = 32 << 30 // 32 GiB
	// maxManifestBytes bounds manifest.json itself.
	maxManifestBytes = 8 << 20
)

const (
	restoreDirName    = ".restore"
	pendingDirName    = "pending"
	pendingMarkerName = "pending.json"
	uploadsDirName    = "uploads"
)

// assetDirs are packaged wholesale. The walk is generic: every regular file
// below these directories is included, and symlinks are rejected rather than
// followed outside the data directory.
var assetDirs = []string{"assets", "templates", "outputs"}

// allowedRoots whitelists the top-level names a package may contain. Anything
// else (logs, backups, fonts, OS metadata) is rejected so a restore cannot
// smuggle unexpected files into the data directory.
var allowedRoots = map[string]bool{
	ManifestName: true,
	"db":         true,
	ConfigEntry:  true,
	"assets":     true,
	"templates":  true,
	"outputs":    true,
}

// ErrInvalidPackage marks packages that fail structural or digest validation.
var ErrInvalidPackage = errors.New("invalid backup package")

// ErrNotConfigured is returned when no data directory is available.
var ErrNotConfigured = errors.New("backup is not configured")

// FileEntry is one file recorded in the manifest.
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a backup package. Files lists every payload file except
// the manifest itself.
type Manifest struct {
	Format        string      `json:"format"`
	FormatVersion int         `json:"format_version"`
	ID            string      `json:"id"`
	AppVersion    string      `json:"app_version"`
	SchemaVersion int         `json:"schema_version"`
	CreatedAt     time.Time   `json:"created_at"`
	Platform      string      `json:"platform"`
	Database      string      `json:"database"`
	Config        string      `json:"config,omitempty"`
	Files         []FileEntry `json:"files"`
}

// TotalSize sums the payload file sizes.
func (m Manifest) TotalSize() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// Manager builds and stages backup packages for one data directory.
type Manager struct {
	Paths config.Paths
	// Version is the running application version recorded in manifests.
	Version string
	// Snapshot writes a consistent database copy to dest. It is required for
	// export and nil for restore-only use.
	Snapshot func(ctx context.Context, dest string) error
	// SchemaVersion reports the current database migration level.
	SchemaVersion func(ctx context.Context) (int, error)
	Now           func() time.Time
	Platform      string
}

// New constructs a Manager.
func New(paths config.Paths, version string, snapshot func(context.Context, string) error,
	schemaVersion func(context.Context) (int, error)) *Manager {
	return &Manager{
		Paths:         paths,
		Version:       version,
		Snapshot:      snapshot,
		SchemaVersion: schemaVersion,
		Now:           time.Now,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// CreateTo builds a package into dir and returns its path and manifest.
func (m *Manager) CreateTo(ctx context.Context, dir string) (string, Manifest, error) {
	if m == nil || strings.TrimSpace(m.Paths.Root) == "" {
		return "", Manifest{}, ErrNotConfigured
	}
	if m.Snapshot == nil || m.SchemaVersion == nil {
		return "", Manifest{}, errors.New("backup manager is missing the database snapshot")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", Manifest{}, fmt.Errorf("create backup dir: %w", err)
	}
	work, err := os.MkdirTemp(dir, ".backup-work-")
	if err != nil {
		return "", Manifest{}, fmt.Errorf("create backup work dir: %w", err)
	}
	defer os.RemoveAll(work)

	dbDest := filepath.Join(work, filepath.FromSlash(DatabaseEntry))
	if err := os.MkdirAll(filepath.Dir(dbDest), 0o700); err != nil {
		return "", Manifest{}, err
	}
	if err := m.Snapshot(ctx, dbDest); err != nil {
		return "", Manifest{}, fmt.Errorf("snapshot database: %w", err)
	}
	schemaVersion, err := m.SchemaVersion(ctx)
	if err != nil {
		return "", Manifest{}, fmt.Errorf("read schema version: %w", err)
	}
	now := m.now()
	id, err := newID(now)
	if err != nil {
		return "", Manifest{}, err
	}
	manifest := Manifest{
		Format:        FormatName,
		FormatVersion: FormatVersion,
		ID:            id,
		AppVersion:    m.Version,
		SchemaVersion: schemaVersion,
		CreatedAt:     now.UTC(),
		Platform:      m.Platform,
		Database:      DatabaseEntry,
		Files:         []FileEntry{},
	}

	sources := []sourceFile{{entry: DatabaseEntry, path: dbDest}}
	if info, err := os.Stat(m.Paths.ConfigFile); err == nil && info.Mode().IsRegular() {
		manifest.Config = ConfigEntry
		sources = append(sources, sourceFile{entry: ConfigEntry, path: m.Paths.ConfigFile})
	}
	assetSources, err := m.collectAssets()
	if err != nil {
		return "", Manifest{}, err
	}
	sources = append(sources, assetSources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].entry < sources[j].entry })

	out, err := os.CreateTemp(dir, ".backup-*.zip")
	if err != nil {
		return "", Manifest{}, fmt.Errorf("create backup package: %w", err)
	}
	tmpName := out.Name()
	cleanup := func() {
		out.Close()
		os.Remove(tmpName)
	}
	zw := zip.NewWriter(out)
	for _, src := range sources {
		entry, err := writeZipFile(zw, src.entry, src.path, now)
		if err != nil {
			zw.Close()
			cleanup()
			return "", Manifest{}, err
		}
		manifest.Files = append(manifest.Files, entry)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		zw.Close()
		cleanup()
		return "", Manifest{}, err
	}
	raw = append(raw, '\n')
	header, err := zipHeader(ManifestName, now)
	if err != nil {
		zw.Close()
		cleanup()
		return "", Manifest{}, err
	}
	w, err := zw.CreateHeader(header)
	if err != nil {
		zw.Close()
		cleanup()
		return "", Manifest{}, err
	}
	if _, err := w.Write(raw); err != nil {
		zw.Close()
		cleanup()
		return "", Manifest{}, err
	}
	if err := zw.Close(); err != nil {
		cleanup()
		return "", Manifest{}, err
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return "", Manifest{}, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpName)
		return "", Manifest{}, err
	}
	final := filepath.Join(dir, "tengshou-backup-"+now.Format("20060102-150405")+".zip")
	if _, err := os.Stat(final); err == nil {
		final = filepath.Join(dir, "tengshou-backup-"+now.Format("20060102-150405")+
			fmt.Sprintf("-%d", now.UnixNano()%1_000_000)+".zip")
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return "", Manifest{}, err
	}
	return final, manifest, nil
}

// Export builds a package, streams it to w and removes the temporary file.
func (m *Manager) Export(ctx context.Context, w io.Writer) (Manifest, error) {
	if m == nil {
		return Manifest{}, ErrNotConfigured
	}
	path, manifest, err := m.CreateTo(ctx, m.Paths.Backups)
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

type sourceFile struct {
	entry string
	path  string
}

// collectAssets walks the user asset directories relative to the data root.
func (m *Manager) collectAssets() ([]sourceFile, error) {
	out := []sourceFile{}
	for _, dirName := range assetDirs {
		root := filepath.Join(m.Paths.Root, dirName)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("read %s: %w", p, err)
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w：备份不支持符号链接 %s", ErrInvalidPackage, p)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%w：不支持的文件类型 %s", ErrInvalidPackage, p)
			}
			rel, err := filepath.Rel(m.Paths.Root, p)
			if err != nil {
				return err
			}
			entry := filepath.ToSlash(rel)
			if strings.HasPrefix(entry, "../") || entry == ".." {
				return fmt.Errorf("%w：路径越界 %s", ErrInvalidPackage, p)
			}
			out = append(out, sourceFile{entry: entry, path: p})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func writeZipFile(zw *zip.Writer, entry, src string, now time.Time) (FileEntry, error) {
	f, err := os.Open(src)
	if err != nil {
		return FileEntry{}, fmt.Errorf("open %s: %w", src, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return FileEntry{}, err
	}
	header, err := zipHeader(entry, info.ModTime())
	if err != nil {
		return FileEntry{}, err
	}
	if !compressible(entry) {
		header.Method = zip.Store
	}
	w, err := zw.CreateHeader(header)
	if err != nil {
		return FileEntry{}, err
	}
	h := sha256.New()
	n, err := io.Copy(w, io.TeeReader(f, h))
	if err != nil {
		return FileEntry{}, fmt.Errorf("write %s: %w", entry, err)
	}
	if n != info.Size() {
		return FileEntry{}, fmt.Errorf("%w：%s 在备份过程中被修改", ErrInvalidPackage, entry)
	}
	return FileEntry{Path: entry, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func zipHeader(name string, modified time.Time) (*zip.FileHeader, error) {
	if err := validArchivePath(name); err != nil {
		return nil, err
	}
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o600)
	if modified.IsZero() {
		modified = time.Now()
	}
	if modified.Year() < 1980 {
		modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	header.Modified = modified
	return header, nil
}

func compressible(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".docx", ".xlsx", ".zip", ".gz", ".webp":
		return false
	}
	return true
}

func newID(now time.Time) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate backup id: %w", err)
	}
	return "bkp_" + now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

// validArchivePath rejects absolute paths, drive letters, backslashes, parent
// traversal and non-canonical names.
func validArchivePath(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%w：非法路径 %q", ErrInvalidPackage, name)
	}
	if strings.ContainsAny(name, "\\") {
		return fmt.Errorf("%w：路径不能包含反斜杠 %q", ErrInvalidPackage, name)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w：不能使用绝对路径 %q", ErrInvalidPackage, name)
	}
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("%w：不能使用盘符路径 %q", ErrInvalidPackage, name)
	}
	clean := path.Clean(name)
	if clean != name {
		return fmt.Errorf("%w：路径不规范 %q", ErrInvalidPackage, name)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w：路径越界 %q", ErrInvalidPackage, name)
	}
	root := strings.SplitN(clean, "/", 2)[0]
	if !allowedRoots[root] {
		return fmt.Errorf("%w：不允许的路径 %q", ErrInvalidPackage, name)
	}
	return nil
}
