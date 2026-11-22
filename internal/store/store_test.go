package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
	_ "modernc.org/sqlite"
)

func mustReader(t *testing.T, st *store.Store) *store.Reader {
	t.Helper()
	r, err := st.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func openStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tengshou.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, path
}

func TestMigrationsOnEmptyDatabase(t *testing.T) {
	st, _ := openStore(t)
	version, err := st.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Fatalf("schema version = %d, want 6", version)
	}
	// Re-opening is idempotent.
	st2, err := store.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if v, _ := st2.SchemaVersion(); v != 6 {
		t.Fatalf("version after reopen = %d", v)
	}
}

func TestMigrationFromOldDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE app_meta`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_contracts_status`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = 2`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("migrate old db: %v", err)
	}
	defer reopened.Close()
	if v, _ := reopened.SchemaVersion(); v != 6 {
		t.Fatalf("version = %d", v)
	}
	db2, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var name string
	if err := db2.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='app_meta'`).Scan(&name); err != nil {
		t.Fatalf("app_meta not recreated: %v", err)
	}
}

func TestPartyContractRoundTripAndPersistence(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	partyA := model.Party{Kind: model.PartyOrganization, Name: "幸福村合作社", Data: map[string]any{"code": "N001"}}
	partyB := model.Party{Kind: model.PartyPerson, Name: "张三", IDNumber: "11010119900101001X"}
	if err := st.PutParty(ctx, &partyA); err != nil {
		t.Fatal(err)
	}
	firstReader := mustReader(t, st)
	gotPartyA, err := firstReader.GetParty(ctx, partyA.ID)
	if err != nil {
		t.Fatal(err)
	}
	partyA = gotPartyA
	if err := st.PutParty(ctx, &partyB); err != nil {
		t.Fatal(err)
	}
	contract := model.Contract{
		Title: "租赁合同", PartyAID: partyA.ID, PartyBID: partyB.ID, Status: model.ContractActive,
		Data: map[string]any{"rent": 1200.0, "address": "幸福村 12 号"},
	}
	if err := st.PutContract(ctx, &contract); err != nil {
		t.Fatal(err)
	}
	detail, err := mustReader(t, st).GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.PartyA == nil || detail.PartyA.Name != "幸福村合作社" {
		t.Fatalf("party A = %+v", detail.PartyA)
	}
	if got := detail.Contract.Data["rent"]; got != 1200.0 {
		t.Fatalf("rent = %v", got)
	}

	st.Close()

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	r2 := mustReader(t, reopened)
	detail2, err := r2.GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatalf("after restart: %v", err)
	}
	if detail2.PartyB == nil || detail2.PartyB.Name != "张三" {
		t.Fatalf("party B after restart = %+v", detail2.PartyB)
	}
}

func TestReaderIsReadOnly(t *testing.T) {
	st, _ := openStore(t)
	reader := mustReader(t, st)
	if err := reader.VerifyReadOnly(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureAndDocument(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	sig := model.Signature{
		PartyID: "", SignerName: "张三", AssetPath: "assets/sig.png",
		SHA256: "abc", WidthPx: 100, HeightPx: 40, CapturedAt: time.Now().UTC(),
	}
	if err := st.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}
	doc := model.OutputDocument{
		ContractID: "", TemplateID: "", TemplateVersion: 1,
		FilePath: "outputs/a.pdf", SHA256: "def", PageCount: 2,
	}
	if err := st.PutOutputDocument(ctx, &doc); err != nil {
		t.Fatal(err)
	}
	reader := mustReader(t, st)
	sigs, err := reader.ListSignatures(ctx, model.ListFilter{})
	if err != nil || sigs.Total != 1 {
		t.Fatalf("signatures = %+v err=%v", sigs, err)
	}
	docs, err := reader.ListDocuments(ctx, model.ListFilter{Query: ""})
	if err != nil || docs.Total != 1 {
		t.Fatalf("documents = %+v err=%v", docs, err)
	}
	stats, err := reader.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Signatures != 1 || stats.Documents != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestTemplateVersionsAndFilters(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	for _, v := range []int{1, 2} {
		tpl := model.Template{ID: "tpl_v" + string(rune('0'+v)), Name: "租赁模板", Version: v,
			HTML: "<p>{{.Data.rent}}</p>", Variables: []string{"rent"}, IsActive: v == 2}
		if err := st.PutTemplate(ctx, &tpl); err != nil {
			t.Fatal(err)
		}
	}
	reader := mustReader(t, st)
	list, err := reader.ListTemplates(ctx, model.ListFilter{})
	if err != nil || list.Total != 2 {
		t.Fatalf("templates = %+v err=%v", list, err)
	}
	for _, item := range list.Items {
		if item.HTML != "" {
			t.Fatal("list should omit template HTML")
		}
	}
	versions, err := reader.ListTemplateVersions(ctx, "租赁模板")
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %+v err=%v", versions, err)
	}
	full, err := reader.GetTemplate(ctx, "tpl_v2")
	if err != nil || full.HTML == "" {
		t.Fatalf("detail template = %+v err=%v", full, err)
	}
}

func TestBackup(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	if err := st.PutParty(ctx, &model.Party{Name: "李四"}); err != nil {
		t.Fatal(err)
	}
	dest, err := st.Backup(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	backupStore, err := store.Open(dest)
	if err != nil {
		t.Fatalf("backup is not a valid database: %v", err)
	}
	defer backupStore.Close()
	reader, err := backupStore.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	stats, err := reader.Stats(ctx)
	if err != nil || stats.Parties != 1 {
		t.Fatalf("backup stats = %+v err=%v", stats, err)
	}
}

func TestMigrationUpgradesB1Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// Minimal B1 schema: contracts without B2 columns, migrations 1 and 2
	// already recorded. Legacy rows must survive the upgrade.
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations VALUES (1, '0001_init.sql', '2026-11-01T00:00:00Z');
		INSERT INTO schema_migrations VALUES (2, '0002_api_indexes.sql', '2026-11-01T00:00:00Z');
		CREATE TABLE contracts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			template_id TEXT,
			party_a_id TEXT,
			party_b_id TEXT,
			status TEXT NOT NULL DEFAULT 'draft',
			signed_on TEXT NOT NULL DEFAULT '',
			valid_from TEXT NOT NULL DEFAULT '',
			valid_to TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO contracts (id, title, status, signed_on, data_json, created_at, updated_at)
		VALUES ('legacy-1', '老合同', 'active', '2024-05-01', '{"rent":800}',
		        '2024-05-01T09:00:00Z', '2024-05-01T09:00:00Z');
		CREATE TABLE signature_assets (
			id TEXT PRIMARY KEY,
			party_id TEXT,
			signer_name TEXT NOT NULL DEFAULT '',
			asset_path TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			width_px INTEGER NOT NULL DEFAULT 0,
			height_px INTEGER NOT NULL DEFAULT 0,
			captured_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		INSERT INTO signature_assets (id, party_id, signer_name, asset_path, sha256,
			width_px, height_px, captured_at, created_at)
		VALUES ('sig-legacy', NULL, '张三', 'assets/sig.png', 'abc', 10, 5,
		        '2024-05-01T09:00:00Z', '2024-05-01T09:00:00Z');
		CREATE TABLE templates (
			id             TEXT PRIMARY KEY,
			name           TEXT NOT NULL,
			version        INTEGER NOT NULL DEFAULT 1,
			html           TEXT NOT NULL,
			variables_json TEXT NOT NULL DEFAULT '[]',
			is_active      INTEGER NOT NULL DEFAULT 1,
			created_at     TEXT NOT NULL,
			updated_at     TEXT NOT NULL,
			UNIQUE (name, version)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade B1 database: %v", err)
	}
	defer st.Close()
	reader := mustReader(t, st)
	ctx := context.Background()
	contract, err := reader.GetContract(ctx, "legacy-1")
	if err != nil {
		t.Fatal(err)
	}
	if contract.ContractNo != "LEGACY-legacy-1" || contract.Year != 2024 {
		t.Fatalf("backfill = %q %d", contract.ContractNo, contract.Year)
	}
	if contract.Data["rent"] != 800.0 {
		t.Fatalf("legacy data lost: %#v", contract.Data)
	}
	fields, err := reader.ListFieldDefinitions(ctx, model.FieldEntityContract, false)
	if err != nil || len(fields) != 10 {
		t.Fatalf("seeded contract fields = %d err=%v", len(fields), err)
	}
	dup := model.Contract{ID: "legacy-2", ContractNo: "LEGACY-legacy-1", Year: 2024, Title: "重复"}
	if err := st.PutContract(ctx, &dup); !errors.Is(err, store.ErrDuplicateContractNo) {
		t.Fatalf("duplicate number error = %v", err)
	}
	sigs, err := reader.ListSignatures(ctx, model.ListFilter{})
	if err != nil || sigs.Total != 1 {
		t.Fatalf("legacy signature = %+v err=%v", sigs, err)
	}
	if sigs.Items[0].ContractID != "" || sigs.Items[0].ContentSHA256 != "" {
		t.Fatalf("legacy signature should have empty B5 links: %+v", sigs.Items[0])
	}
}
