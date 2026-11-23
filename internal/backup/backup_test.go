package backup_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/backup"
	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func writeAsset(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newSource(t *testing.T) (config.Paths, *store.Store, model.Party) {
	t.Helper()
	paths, err := config.ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HTTP.Token = "source-token"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	party := model.Party{Kind: model.PartyPerson, Name: "张三", Phone: "13800000000"}
	if err := st.PutParty(ctx, &party); err != nil {
		t.Fatal(err)
	}
	writeAsset(t, filepath.Join(paths.Assets, "signatures", "sig_a.png"), []byte("signature-png"))
	writeAsset(t, filepath.Join(paths.Assets, "seals", "seal_a.png"), []byte("seal-png"))
	writeAsset(t, filepath.Join(paths.Templates, "pdf", "tpl_1", "source.docx"), []byte("word-source"))
	writeAsset(t, filepath.Join(paths.Templates, "pdf", "tpl_1", "template.pdf"), []byte("%PDF-template"))
	writeAsset(t, filepath.Join(paths.Outputs, "out.pdf"), []byte("%PDF-output"))
	return paths, st, party
}

func newManager(paths config.Paths, st *store.Store) *backup.Manager {
	return backup.New(paths, "test-1.0",
		func(ctx context.Context, dest string) error { return st.Snapshot(dest) },
		func(ctx context.Context) (int, error) { return st.SchemaVersion() })
}

func TestBackupRoundTrip(t *testing.T) {
	ctx := context.Background()
	srcPaths, st, party := newSource(t)
	zipPath, manifest, err := newManager(srcPaths, st).CreateTo(ctx, srcPaths.Backups)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	t.Cleanup(func() { os.Remove(zipPath) })
	if manifest.Format != backup.FormatName || manifest.FormatVersion != backup.FormatVersion {
		t.Fatalf("manifest format = %+v", manifest)
	}
	if manifest.AppVersion != "test-1.0" || manifest.SchemaVersion <= 0 || manifest.Platform == "" {
		t.Fatalf("manifest metadata = %+v", manifest)
	}
	listed := map[string]bool{}
	for _, entry := range manifest.Files {
		listed[entry.Path] = true
	}
	for _, want := range []string{
		"db/tengshou.db", "config.json",
		"assets/signatures/sig_a.png", "assets/seals/seal_a.png",
		"templates/pdf/tpl_1/source.docx", "templates/pdf/tpl_1/template.pdf",
		"outputs/out.pdf",
	} {
		if !listed[want] {
			t.Fatalf("manifest missing %s: %+v", want, manifest.Files)
		}
	}
	assertZipMatchesManifest(t, zipPath, manifest)

	// Restore into an empty data directory, exactly like a new machine.
	dstPaths, err := config.ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dstMgr := backup.New(dstPaths, "test-1.0", nil, nil)
	staged, err := dstMgr.Stage(ctx, f, 1<<20)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if staged.ID != manifest.ID || staged.SchemaVersion != manifest.SchemaVersion {
		t.Fatalf("staged manifest = %+v", staged)
	}
	pending, ok, err := dstMgr.Pending()
	if err != nil || !ok || pending.Manifest.ID != manifest.ID {
		t.Fatalf("pending = %+v ok=%v err=%v", pending, ok, err)
	}
	res, err := backup.ApplyPending(dstPaths)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied || res.Manifest.ID != manifest.ID {
		t.Fatalf("result = %+v", res)
	}
	dstStore, err := store.Open(dstPaths.DB)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer dstStore.Close()
	rd, err := dstStore.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	restored, err := rd.GetParty(ctx, party.ID)
	if err != nil || restored.Name != "张三" {
		t.Fatalf("restored party = %+v err=%v", restored, err)
	}
	raw, err := os.ReadFile(filepath.Join(dstPaths.Assets, "signatures", "sig_a.png"))
	if err != nil || string(raw) != "signature-png" {
		t.Fatalf("restored signature = %q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dstPaths.Templates, "pdf", "tpl_1", "source.docx")); err != nil {
		t.Fatalf("restored nested template file: %v", err)
	}
	restoredCfg, err := config.Load(dstPaths)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if restoredCfg.HTTP.Token != "source-token" {
		t.Fatalf("restored token = %q", restoredCfg.HTTP.Token)
	}
}

func assertZipMatchesManifest(t *testing.T, path string, manifest backup.Manifest) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	if _, ok := files["manifest.json"]; !ok {
		t.Fatal("package has no manifest.json")
	}
	for _, entry := range manifest.Files {
		f, ok := files[entry.Path]
		if !ok {
			t.Fatalf("package missing %s", entry.Path)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
		sum := sha256.Sum256(buf.Bytes())
		if int64(buf.Len()) != entry.Size || hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("entry %s size/digest mismatch", entry.Path)
		}
	}
}

// buildPackage writes a zip containing the given payload files plus a manifest
// derived from them. manifestOverride may adjust entries before hashing.
func buildPackage(t *testing.T, payload map[string][]byte,
	override func(*backup.Manifest)) string {
	t.Helper()
	manifest := backup.Manifest{
		Format:        backup.FormatName,
		FormatVersion: backup.FormatVersion,
		ID:            "bkp_test",
		AppVersion:    "test",
		SchemaVersion: store.LatestSchemaVersion(),
		Platform:      "test/test",
		Database:      backup.DatabaseEntry,
	}
	names := make([]string, 0, len(payload))
	for name := range payload {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sum := sha256.Sum256(payload[name])
		manifest.Files = append(manifest.Files, backup.FileEntry{
			Path: name, Size: int64(len(payload[name])), SHA256: hex.EncodeToString(sum[:]),
		})
	}
	if override != nil {
		override(&manifest)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pkg.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func validDB(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func stagePackage(t *testing.T, zipPath string) error {
	t.Helper()
	return stagePackageLimit(t, zipPath, 1<<20)
}

func stagePackageLimit(t *testing.T, zipPath string, maxBytes int64) error {
	t.Helper()
	paths, err := config.ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := backup.New(paths, "test", nil, nil)
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = mgr.Stage(context.Background(), f, maxBytes)
	return err
}

func TestStageRejectsMissingFile(t *testing.T) {
	db := validDB(t)
	payload := map[string][]byte{"db/tengshou.db": db, "assets/a.png": []byte("a")}
	path := buildPackage(t, payload, func(m *backup.Manifest) {
		m.Files = append(m.Files, backup.FileEntry{
			Path: "assets/missing.png", Size: 3, SHA256: strings.Repeat("0", 64),
		})
	})
	err := stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "缺少文件") {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestStageRejectsHashMismatch(t *testing.T) {
	db := validDB(t)
	payload := map[string][]byte{"db/tengshou.db": db, "assets/a.png": []byte("a")}
	path := buildPackage(t, payload, func(m *backup.Manifest) {
		m.Files[1].SHA256 = strings.Repeat("0", 64)
	})
	err := stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "摘要不符") {
		t.Fatalf("hash mismatch error = %v", err)
	}
}

func TestStageRejectsIllegalZip(t *testing.T) {
	db := validDB(t)
	// A malicious package can inject any zip entry name, bypassing the
	// manifest builder, so build it by hand.
	path := buildPackagePayloadWithTraversal(t, db)
	err := stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "越界") {
		t.Fatalf("illegal zip error = %v", err)
	}
}

func buildPackagePayloadWithTraversal(t *testing.T, db []byte) string {
	t.Helper()
	manifest := backup.Manifest{
		Format: backup.FormatName, FormatVersion: backup.FormatVersion,
		ID: "bkp_bad", AppVersion: "test", SchemaVersion: store.LatestSchemaVersion(),
		Platform: "test/test", Database: backup.DatabaseEntry,
		Files: []backup.FileEntry{{Path: "db/tengshou.db", Size: int64(len(db)),
			SHA256: sha256Hex(db)}},
	}
	raw, _ := json.Marshal(manifest)
	path := filepath.Join(t.TempDir(), "evil.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	write := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", raw)
	write("db/tengshou.db", db)
	write("../evil.txt", []byte("x"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestStageRejectsCorruptDatabase(t *testing.T) {
	payload := map[string][]byte{"db/tengshou.db": []byte("not a sqlite database")}
	path := buildPackage(t, payload, nil)
	err := stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "完整性检查失败") {
		t.Fatalf("corrupt database error = %v", err)
	}
}

func TestStageRejectsGarbage(t *testing.T) {
	paths, err := config.ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := backup.New(paths, "test", nil, nil)
	_, err = mgr.Stage(context.Background(), strings.NewReader("definitely not a zip"), 1<<20)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) {
		t.Fatalf("garbage error = %v", err)
	}
}

func TestApplyPendingFailureKeepsOriginal(t *testing.T) {
	ctx := context.Background()
	paths, st, party := newSource(t)
	zipPath, manifest, err := newManager(paths, st).CreateTo(ctx, paths.Backups)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	mgr := backup.New(paths, "test", nil, nil)
	if _, err := mgr.Stage(ctx, f, 1<<20); err != nil {
		t.Fatalf("stage: %v", err)
	}
	// Tamper with the staged database after validation. ApplyPending must
	// notice, refuse the restore and leave the live data untouched.
	pendingDB := filepath.Join(paths.Root, ".restore", "pending", "db", "tengshou.db")
	if err := os.WriteFile(pendingDB, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.ApplyPending(paths); err == nil {
		t.Fatal("tampered staged restore should fail")
	}
	rd, err := st.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	if _, err := rd.GetParty(ctx, party.ID); err != nil {
		t.Fatalf("original party lost after failed restore: %v", err)
	}
	if _, ok, _ := mgr.Pending(); ok {
		t.Fatal("failed staged restore should be cleaned up")
	}
	if manifest.ID == "" {
		t.Fatal("empty manifest id")
	}
}

// F-TE-1: restoring over a non-empty data directory must first write a
// pre-restore safety copy holding exactly the data the restore replaces.
func TestApplyPendingWritesPreRestoreSafetyCopy(t *testing.T) {
	ctx := context.Background()
	srcPaths, st, party := newSource(t)
	zipPath, manifest, err := newManager(srcPaths, st).CreateTo(ctx, srcPaths.Backups)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	dstPaths, err := config.ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The destination already holds live data that the restore will replace.
	oldStore, err := store.Open(dstPaths.DB)
	if err != nil {
		t.Fatal(err)
	}
	oldParty := model.Party{Kind: model.PartyPerson, Name: "旧数据"}
	if err := oldStore.PutParty(ctx, &oldParty); err != nil {
		t.Fatal(err)
	}
	if err := oldStore.Close(); err != nil {
		t.Fatal(err)
	}
	oldAsset := filepath.Join(dstPaths.Assets, "signatures", "old.png")
	writeAsset(t, oldAsset, []byte("old-signature"))
	oldDB, err := os.ReadFile(dstPaths.DB)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	dstMgr := backup.New(dstPaths, "test-1.0", nil, nil)
	_, stageErr := dstMgr.Stage(ctx, f, 1<<20)
	f.Close()
	if stageErr != nil {
		t.Fatalf("stage: %v", stageErr)
	}
	res, err := backup.ApplyPending(dstPaths)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied || res.Manifest.ID != manifest.ID {
		t.Fatalf("result = %+v", res)
	}
	if res.SafetyDir == "" {
		t.Fatal("restore result has no safety dir")
	}
	if info, err := os.Stat(res.SafetyDir); err != nil || !info.IsDir() {
		t.Fatalf("safety dir = %q err=%v", res.SafetyDir, err)
	}
	safetyDB, err := os.ReadFile(filepath.Join(res.SafetyDir, "tengshou.db"))
	if err != nil || !bytes.Equal(safetyDB, oldDB) {
		t.Fatalf("safety database copy mismatch: err=%v", err)
	}
	safetyAsset, err := os.ReadFile(filepath.Join(res.SafetyDir, "assets", "signatures", "old.png"))
	if err != nil || string(safetyAsset) != "old-signature" {
		t.Fatalf("safety asset copy = %q err=%v", safetyAsset, err)
	}

	// The restore itself replaced the live data with the package contents.
	restoredStore, err := store.Open(dstPaths.DB)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restoredStore.Close()
	rd, err := restoredStore.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	restored, err := rd.GetParty(ctx, party.ID)
	if err != nil || restored.Name != "张三" {
		t.Fatalf("restored party = %+v err=%v", restored, err)
	}
	if _, err := rd.GetParty(ctx, oldParty.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pre-restore party should be gone: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dstPaths.Assets, "signatures", "sig_a.png")); err != nil ||
		string(raw) != "signature-png" {
		t.Fatalf("restored signature = %q err=%v", raw, err)
	}
	if _, err := os.Stat(oldAsset); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old signature should be replaced by the restore: %v", err)
	}
}

// F-TE-2: a package with more entries than MaxEntries is refused before any
// entry is extracted.
func TestStageRejectsTooManyEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for i := 0; i <= backup.MaxEntries; i++ {
		if _, err := zw.Create(fmt.Sprintf("assets/e%d", i)); err != nil {
			t.Fatalf("write entry %d: %v", i, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	// The upload cap is raised on purpose: the guard under test is the entry
	// count, not the transport size.
	err = stagePackageLimit(t, path, 64<<20)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "条目数量超过上限") {
		t.Fatalf("entry-count error = %v", err)
	}
}

// F-TE-2: a package that only *declares* more uncompressed bytes than
// MaxTotalUncompressed is refused without extracting the payload.
func TestStageRejectsDeclaredSizeBomb(t *testing.T) {
	db := validDB(t)
	manifest := backup.Manifest{
		Format: backup.FormatName, FormatVersion: backup.FormatVersion,
		ID: "bkp_bomb", AppVersion: "test", SchemaVersion: store.LatestSchemaVersion(),
		Platform: "test/test", Database: backup.DatabaseEntry,
		Files: []backup.FileEntry{{Path: "db/tengshou.db", Size: int64(len(db)),
			SHA256: sha256Hex(db)}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bomb.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	write := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", raw)
	write("db/tengshou.db", db)
	// One raw entry carries a one-byte payload but declares an uncompressed
	// size past the guard, so the package must be refused up front.
	bomb := &zip.FileHeader{
		Name: "assets/bomb.bin", Method: zip.Store,
		CompressedSize64: 1, UncompressedSize64: uint64(backup.MaxTotalUncompressed) + 1,
	}
	w, err := zw.CreateRaw(bomb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	err = stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "解压后大小超过上限") {
		t.Fatalf("declared-size error = %v", err)
	}
}

// F-TE-3: packages whose schema is newer than this application are refused.
func TestStageRejectsNewerSchema(t *testing.T) {
	payload := map[string][]byte{"db/tengshou.db": validDB(t)}
	path := buildPackage(t, payload, func(m *backup.Manifest) {
		m.SchemaVersion = store.LatestSchemaVersion() + 1
	})
	err := stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "高于当前应用支持的") {
		t.Fatalf("newer schema error = %v", err)
	}
}

// F-TE-3: symlink entries are refused; a restore must never create links.
func TestStageRejectsSymlinkEntry(t *testing.T) {
	manifest := backup.Manifest{
		Format: backup.FormatName, FormatVersion: backup.FormatVersion,
		ID: "bkp_link", AppVersion: "test", SchemaVersion: store.LatestSchemaVersion(),
		Platform: "test/test", Database: backup.DatabaseEntry,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "symlink.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "assets/link", Method: zip.Deflate}
	header.SetMode(os.ModeSymlink | 0o777)
	w, err = zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("target.txt")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	err = stagePackage(t, path)
	if err == nil || !errors.Is(err, backup.ErrInvalidPackage) ||
		!strings.Contains(err.Error(), "不允许符号链接") {
		t.Fatalf("symlink error = %v", err)
	}
}
