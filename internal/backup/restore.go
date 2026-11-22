package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// PendingInfo describes a validated package waiting for the next startup.
type PendingInfo struct {
	StagedAt time.Time `json:"staged_at"`
	Manifest Manifest  `json:"manifest"`
}

// Result reports an applied restore.
type Result struct {
	Applied   bool
	Manifest  Manifest
	SafetyDir string
}

// Stage validates a package from src and stages it for the next startup. The
// current data is never touched: invalid packages leave it exactly as it was.
func (m *Manager) Stage(ctx context.Context, src io.Reader, maxBytes int64) (Manifest, error) {
	if m == nil || strings.TrimSpace(m.Paths.Root) == "" {
		return Manifest{}, ErrNotConfigured
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxUploadBytes
	}
	base := filepath.Join(m.Paths.Root, restoreDirName)
	uploads := filepath.Join(base, uploadsDirName)
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create restore upload dir: %w", err)
	}
	upload, err := os.CreateTemp(uploads, "upload-*.zip")
	if err != nil {
		return Manifest{}, fmt.Errorf("create restore upload: %w", err)
	}
	uploadName := upload.Name()
	defer os.Remove(uploadName)
	written, err := io.Copy(upload, io.LimitReader(src, maxBytes+1))
	if err != nil {
		upload.Close()
		return Manifest{}, fmt.Errorf("receive backup package: %w", err)
	}
	if written > maxBytes {
		upload.Close()
		return Manifest{}, fmt.Errorf("%w：备份包超过上传上限", ErrInvalidPackage)
	}
	if err := upload.Sync(); err != nil {
		upload.Close()
		return Manifest{}, err
	}
	if err := upload.Close(); err != nil {
		return Manifest{}, err
	}

	staging, err := os.MkdirTemp(base, "staging-")
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := extractAndVerify(uploadName, staging)
	if err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}

	pending := filepath.Join(base, pendingDirName)
	marker := filepath.Join(base, pendingMarkerName)
	if err := os.RemoveAll(pending); err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	if err := os.Rename(staging, pending); err != nil {
		os.RemoveAll(staging)
		return Manifest{}, fmt.Errorf("stage restore: %w", err)
	}
	info := PendingInfo{StagedAt: m.now().UTC(), Manifest: manifest}
	if err := writeMarker(marker, info); err != nil {
		os.RemoveAll(pending)
		return Manifest{}, err
	}
	return manifest, nil
}

// Pending reports whether a validated package is waiting for restart.
func (m *Manager) Pending() (PendingInfo, bool, error) {
	if m == nil || strings.TrimSpace(m.Paths.Root) == "" {
		return PendingInfo{}, false, nil
	}
	marker := filepath.Join(m.Paths.Root, restoreDirName, pendingMarkerName)
	raw, err := os.ReadFile(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return PendingInfo{}, false, nil
	}
	if err != nil {
		return PendingInfo{}, false, err
	}
	var info PendingInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return PendingInfo{}, false, fmt.Errorf("%w：恢复标记损坏", ErrInvalidPackage)
	}
	return info, true, nil
}

// ApplyPending swaps a staged package into the data directory. It must run
// before the database is opened (the application calls it at startup). On any
// failure the current data is left untouched; a pre-restore safety copy is
// written before anything is moved.
func ApplyPending(paths config.Paths) (Result, error) {
	root := strings.TrimSpace(paths.Root)
	if root == "" {
		return Result{}, nil
	}
	base := filepath.Join(root, restoreDirName)
	pending := filepath.Join(base, pendingDirName)
	marker := filepath.Join(base, pendingMarkerName)
	_, pendingErr := os.Stat(pending)
	_, markerErr := os.Stat(marker)
	if errors.Is(pendingErr, fs.ErrNotExist) && errors.Is(markerErr, fs.ErrNotExist) {
		return Result{}, nil
	}
	if pendingErr != nil || markerErr != nil {
		// A half-written staging area is never applied.
		os.RemoveAll(pending)
		os.Remove(marker)
		return Result{}, fmt.Errorf("%w：恢复暂存不完整，已清理；原数据未改动", ErrInvalidPackage)
	}
	// A leftover old-* directory means a previous swap was interrupted by a
	// crash. Roll the original items back before doing anything else.
	if recovered, err := recoverInterruptedSwap(root, base); err != nil {
		return Result{}, fmt.Errorf("上次恢复中断，回滚失败：%w", err)
	} else if recovered {
		os.RemoveAll(pending)
		os.Remove(marker)
		return Result{}, errors.New("上次恢复在替换过程中中断，已回滚到原数据；请重新选择备份包恢复")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return Result{}, err
	}
	var info PendingInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return Result{}, fmt.Errorf("%w：恢复标记损坏", ErrInvalidPackage)
	}
	if info.Manifest.Format != FormatName || info.Manifest.FormatVersion != FormatVersion {
		return Result{}, fmt.Errorf("%w：不支持的备份包格式", ErrInvalidPackage)
	}
	if err := verifyTree(pending, info.Manifest); err != nil {
		os.RemoveAll(pending)
		os.Remove(marker)
		return Result{}, err
	}
	dbStaged := filepath.Join(pending, filepath.FromSlash(info.Manifest.Database))
	if err := store.IntegrityCheckFile(dbStaged); err != nil {
		os.RemoveAll(pending)
		os.Remove(marker)
		return Result{}, fmt.Errorf("%w：恢复前数据库校验失败：%v", ErrInvalidPackage, err)
	}

	now := time.Now()
	safety := filepath.Join(paths.Backups, "pre-restore-"+now.Format("20060102-150405"))
	if err := os.MkdirAll(safety, 0o700); err != nil {
		return Result{}, fmt.Errorf("create pre-restore safety dir: %w", err)
	}
	if err := copyCurrentData(paths, safety); err != nil {
		os.RemoveAll(safety)
		return Result{}, fmt.Errorf("write pre-restore safety copy: %w", err)
	}

	oldDir := filepath.Join(base, "old-"+now.Format("20060102-150405.000000000"))
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		os.RemoveAll(safety)
		return Result{}, err
	}
	current := currentItems(paths)
	moved := []swapItem{}
	placedNames := []string{}
	rollback := func() {
		for _, name := range placedNames {
			os.RemoveAll(filepath.Join(root, name))
		}
		for _, item := range moved {
			os.Rename(filepath.Join(oldDir, item.name), item.abs)
		}
	}
	for _, item := range current {
		if _, err := os.Lstat(item.abs); err != nil {
			continue
		}
		if err := os.Rename(item.abs, filepath.Join(oldDir, item.name)); err != nil {
			rollback()
			return Result{}, fmt.Errorf("move current %s aside: %w", item.name, err)
		}
		moved = append(moved, item)
	}
	staged := []swapItem{
		{name: filepath.Base(paths.DB), abs: paths.DB,
			staged: filepath.Join(pending, filepath.FromSlash(info.Manifest.Database))},
	}
	if info.Manifest.Config != "" {
		staged = append(staged, swapItem{name: filepath.Base(paths.ConfigFile), abs: paths.ConfigFile,
			staged: filepath.Join(pending, filepath.FromSlash(info.Manifest.Config))})
	}
	for _, dirName := range assetDirs {
		final := filepath.Join(root, dirName)
		staged = append(staged, swapItem{name: dirName, abs: final,
			staged: filepath.Join(pending, dirName)})
	}
	for _, item := range staged {
		if _, err := os.Lstat(item.staged); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			rollback()
			return Result{}, err
		}
		if err := os.MkdirAll(filepath.Dir(item.abs), 0o700); err != nil {
			rollback()
			return Result{}, err
		}
		if err := os.Rename(item.staged, item.abs); err != nil {
			rollback()
			return Result{}, fmt.Errorf("apply restored %s: %w", item.name, err)
		}
		placedNames = append(placedNames, item.name)
	}
	os.RemoveAll(oldDir)
	os.RemoveAll(pending)
	os.Remove(marker)
	uploads := filepath.Join(base, uploadsDirName)
	if entries, err := os.ReadDir(uploads); err == nil {
		for _, entry := range entries {
			os.RemoveAll(filepath.Join(uploads, entry.Name()))
		}
	}
	_ = paths.EnsureDirs()
	return Result{Applied: true, Manifest: info.Manifest, SafetyDir: safety}, nil
}

type swapItem struct {
	name   string
	abs    string
	staged string
}

// recoverInterruptedSwap restores the original data from a leftover old-*
// directory. Successful restores always remove that directory before clearing
// the pending marker, so finding one means the previous swap did not finish.
func recoverInterruptedSwap(root, base string) (bool, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return false, nil
	}
	oldDirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "old-") {
			oldDirs = append(oldDirs, entry.Name())
		}
	}
	if len(oldDirs) == 0 {
		return false, nil
	}
	sort.Strings(oldDirs)
	latest := oldDirs[len(oldDirs)-1]
	latestPath := filepath.Join(base, latest)
	items, err := os.ReadDir(latestPath)
	if err != nil {
		return true, err
	}
	for _, item := range items {
		live := filepath.Join(root, item.Name())
		if err := os.RemoveAll(live); err != nil {
			return true, err
		}
		if err := os.Rename(filepath.Join(latestPath, item.Name()), live); err != nil {
			return true, err
		}
	}
	for _, name := range oldDirs {
		os.RemoveAll(filepath.Join(base, name))
	}
	return true, nil
}

func currentItems(paths config.Paths) []swapItem {
	return []swapItem{
		{name: filepath.Base(paths.DB), abs: paths.DB},
		{name: filepath.Base(paths.DB) + "-wal", abs: paths.DB + "-wal"},
		{name: filepath.Base(paths.DB) + "-shm", abs: paths.DB + "-shm"},
		{name: filepath.Base(paths.ConfigFile), abs: paths.ConfigFile},
		{name: "assets", abs: paths.Assets},
		{name: "templates", abs: paths.Templates},
		{name: "outputs", abs: paths.Outputs},
	}
}

// copyCurrentData writes the pre-restore safety copy. Failures abort the
// restore before anything is modified.
func copyCurrentData(paths config.Paths, dest string) error {
	for _, item := range currentItems(paths) {
		if _, err := os.Lstat(item.abs); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		if err := copyPath(item.abs, filepath.Join(dest, item.name)); err != nil {
			return err
		}
	}
	return nil
}

func copyPath(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink %s", src)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dest, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to copy special file %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// extractAndVerify reads the zip, checks the manifest, extracts exactly the
// listed files into dest and verifies every digest before reporting success.
func extractAndVerify(zipPath, dest string) (Manifest, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Manifest{}, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return Manifest{}, fmt.Errorf("%w：文件不是有效的 zip：%v", ErrInvalidPackage, err)
	}
	if len(zr.File) > MaxEntries {
		return Manifest{}, fmt.Errorf("%w：条目数量超过上限", ErrInvalidPackage)
	}
	entries := map[string]*zip.File{}
	var declaredTotal int64
	for _, zf := range zr.File {
		name := zf.Name
		if strings.HasSuffix(name, "/") {
			name = strings.TrimSuffix(name, "/")
			if name != "" {
				if err := validArchivePath(name); err != nil {
					return Manifest{}, err
				}
			}
			continue
		}
		if err := validArchivePath(name); err != nil {
			return Manifest{}, err
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, fmt.Errorf("%w：不允许符号链接 %q", ErrInvalidPackage, name)
		}
		if _, ok := entries[name]; ok {
			return Manifest{}, fmt.Errorf("%w：重复条目 %q", ErrInvalidPackage, name)
		}
		entries[name] = zf
		if int64(zf.UncompressedSize64) > MaxTotalUncompressed ||
			declaredTotal > MaxTotalUncompressed-int64(zf.UncompressedSize64) {
			return Manifest{}, fmt.Errorf("%w：解压后大小超过上限", ErrInvalidPackage)
		}
		declaredTotal += int64(zf.UncompressedSize64)
	}
	manifestFile, ok := entries[ManifestName]
	if !ok {
		return Manifest{}, fmt.Errorf("%w：缺少 manifest.json", ErrInvalidPackage)
	}
	if manifestFile.UncompressedSize64 > maxManifestBytes {
		return Manifest{}, fmt.Errorf("%w：manifest.json 过大", ErrInvalidPackage)
	}
	manifestRaw, err := readZipFile(manifestFile)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w：manifest.json 无法解析", ErrInvalidPackage)
	}
	if manifest.Format != FormatName || manifest.FormatVersion != FormatVersion {
		return Manifest{}, fmt.Errorf("%w：不支持的备份包格式（可能是更新版本的腾手）", ErrInvalidPackage)
	}
	if manifest.Database != DatabaseEntry {
		return Manifest{}, fmt.Errorf("%w：数据库路径不在约定位置", ErrInvalidPackage)
	}
	if manifest.Config != "" && manifest.Config != ConfigEntry {
		return Manifest{}, fmt.Errorf("%w：配置文件路径不在约定位置", ErrInvalidPackage)
	}
	if manifest.SchemaVersion <= 0 {
		return Manifest{}, fmt.Errorf("%w：缺少数据库版本信息", ErrInvalidPackage)
	}
	if latest := store.LatestSchemaVersion(); manifest.SchemaVersion > latest {
		return Manifest{}, fmt.Errorf("%w：备份包数据库版本 v%d 高于当前应用支持的 v%d",
			ErrInvalidPackage, manifest.SchemaVersion, latest)
	}
	listed := map[string]FileEntry{}
	for _, entry := range manifest.Files {
		if err := validArchivePath(entry.Path); err != nil {
			return Manifest{}, err
		}
		if entry.Path == ManifestName {
			return Manifest{}, fmt.Errorf("%w：manifest 不能列出自身", ErrInvalidPackage)
		}
		if entry.Size < 0 {
			return Manifest{}, fmt.Errorf("%w：文件大小非法 %q", ErrInvalidPackage, entry.Path)
		}
		digest, err := hex.DecodeString(strings.TrimSpace(entry.SHA256))
		if err != nil || len(digest) != 32 {
			return Manifest{}, fmt.Errorf("%w：文件摘要非法 %q", ErrInvalidPackage, entry.Path)
		}
		if _, ok := listed[entry.Path]; ok {
			return Manifest{}, fmt.Errorf("%w：manifest 重复列出 %q", ErrInvalidPackage, entry.Path)
		}
		listed[entry.Path] = entry
	}
	for name := range entries {
		if name == ManifestName {
			continue
		}
		if _, ok := listed[name]; !ok {
			return Manifest{}, fmt.Errorf("%w：包内存在未登记文件 %q", ErrInvalidPackage, name)
		}
	}
	for name := range listed {
		if _, ok := entries[name]; !ok {
			return Manifest{}, fmt.Errorf("%w：包内缺少文件 %q", ErrInvalidPackage, name)
		}
	}

	if err := os.MkdirAll(dest, 0o700); err != nil {
		return Manifest{}, err
	}
	if err := writeFileBytes(filepath.Join(dest, ManifestName), manifestRaw); err != nil {
		return Manifest{}, err
	}
	for _, entry := range manifest.Files {
		zf := entries[entry.Path]
		target := filepath.Join(dest, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Manifest{}, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Manifest{}, err
		}
		rc, err := zf.Open()
		if err != nil {
			out.Close()
			return Manifest{}, err
		}
		h := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(rc, entry.Size+1))
		rc.Close()
		if copyErr != nil {
			out.Close()
			return Manifest{}, fmt.Errorf("%w：读取 %q 失败", ErrInvalidPackage, entry.Path)
		}
		if written != entry.Size {
			out.Close()
			return Manifest{}, fmt.Errorf("%w：%q 大小不符（应为 %d，实际 %d）",
				ErrInvalidPackage, entry.Path, entry.Size, written)
		}
		if err := out.Sync(); err != nil {
			out.Close()
			return Manifest{}, err
		}
		if err := out.Close(); err != nil {
			return Manifest{}, err
		}
		if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), entry.SHA256) {
			return Manifest{}, fmt.Errorf("%w：%q 摘要不符", ErrInvalidPackage, entry.Path)
		}
	}
	dbPath := filepath.Join(dest, filepath.FromSlash(manifest.Database))
	if err := store.IntegrityCheckFile(dbPath); err != nil {
		return Manifest{}, fmt.Errorf("%w：数据库完整性检查失败：%v", ErrInvalidPackage, err)
	}
	return manifest, nil
}

// verifyTree re-checks the staged files before applying, so a crash or manual
// tampering between staging and restart cannot silently corrupt the restore.
func verifyTree(root string, manifest Manifest) error {
	listed := map[string]FileEntry{}
	for _, entry := range manifest.Files {
		listed[entry.Path] = entry
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == ManifestName {
			return nil
		}
		entry, ok := listed[name]
		if !ok {
			return fmt.Errorf("%w：暂存目录存在未登记文件 %q", ErrInvalidPackage, name)
		}
		seen[name] = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != entry.Size {
			return fmt.Errorf("%w：暂存文件大小不符 %q", ErrInvalidPackage, name)
		}
		sum, err := fileSHA256(p)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, entry.SHA256) {
			return fmt.Errorf("%w：暂存文件摘要不符 %q", ErrInvalidPackage, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for name := range listed {
		if !seen[name] {
			return fmt.Errorf("%w：暂存目录缺少文件 %q", ErrInvalidPackage, name)
		}
	}
	return nil
}

func readZipFile(zf *zip.File) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(rc, maxManifestBytes+1)); err != nil {
		return nil, err
	}
	if buf.Len() > maxManifestBytes {
		return nil, fmt.Errorf("%w：manifest.json 过大", ErrInvalidPackage)
	}
	return buf.Bytes(), nil
}

func writeFileBytes(dest string, raw []byte) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := out.Write(raw); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeMarker(dest string, info PendingInfo) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := dest + ".tmp"
	os.Remove(tmp)
	if err := writeFileBytes(tmp, raw); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
