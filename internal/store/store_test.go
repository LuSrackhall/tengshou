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
	if version != store.LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", version, store.LatestSchemaVersion())
	}
	// Re-opening is idempotent.
	st2, err := store.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if v, _ := st2.SchemaVersion(); v != store.LatestSchemaVersion() {
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
	if v, _ := reopened.SchemaVersion(); v != store.LatestSchemaVersion() {
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
		CREATE TABLE parties (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			id_number TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO parties (id, kind, name, created_at, updated_at)
		VALUES ('pty-legacy', 'person', '老档案', '2024-05-01T09:00:00Z', '2024-05-01T09:00:00Z');
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
		CREATE TABLE output_documents (
			id               TEXT PRIMARY KEY,
			contract_id      TEXT,
			template_id      TEXT,
			template_version INTEGER NOT NULL DEFAULT 0,
			file_path        TEXT NOT NULL,
			sha256           TEXT NOT NULL,
			page_count       INTEGER NOT NULL DEFAULT 0,
			created_at       TEXT NOT NULL
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
	if err != nil || len(fields) != 12 {
		t.Fatalf("seeded contract fields = %d err=%v", len(fields), err)
	}
	leaseFields := map[string]bool{}
	for _, field := range fields {
		if field.Key == model.FieldLeaseStart || field.Key == model.FieldLeaseEnd {
			leaseFields[field.Key] = field.BuiltIn
		}
	}
	if !leaseFields[model.FieldLeaseStart] || !leaseFields[model.FieldLeaseEnd] {
		t.Fatalf("lease fields missing or not built-in: %#v", leaseFields)
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

// TestContractSourceRoundTrip pins the B7 provenance column: it is written on
// insert, readable through every contract query, and ignored by updates.
func TestContractSourceRoundTrip(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	party := model.Party{Name: "张三"}
	if err := st.PutParty(ctx, &party); err != nil {
		t.Fatal(err)
	}
	source := model.Contract{Title: "2025 房屋租赁合同", Year: 2025, PartyBID: party.ID}
	if err := st.PutContract(ctx, &source); err != nil {
		t.Fatal(err)
	}
	renewed := model.Contract{Title: "2026 房屋租赁合同", Year: 2026,
		PartyBID: party.ID, SourceContractID: source.ID}
	if err := st.PutContract(ctx, &renewed); err != nil {
		t.Fatal(err)
	}
	unrelated := model.Contract{Title: "无关合同", Year: 2026}
	if err := st.PutContract(ctx, &unrelated); err != nil {
		t.Fatal(err)
	}
	reader := mustReader(t, st)
	got, err := reader.GetContract(ctx, renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceContractID != source.ID {
		t.Fatalf("source_contract_id = %q, want %q", got.SourceContractID, source.ID)
	}
	// Party history filtering uses either side of the contract.
	byParty, err := reader.ListContracts(ctx, model.ListFilter{PartyID: party.ID})
	if err != nil {
		t.Fatal(err)
	}
	if byParty.Total != 2 {
		t.Fatalf("party filter total = %d, want 2", byParty.Total)
	}

	// Updates ignore the provenance column: clearing it in the input keeps the
	// stored link instead of erasing the audit trail.
	got.SourceContractID = ""
	got.Title = "2026 房屋租赁合同（修订）"
	if err := st.PutContract(ctx, &got); err != nil {
		t.Fatal(err)
	}
	again, err := reader.GetContract(ctx, renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.SourceContractID != source.ID {
		t.Fatalf("update erased source link: %q", again.SourceContractID)
	}
	if again.Title != "2026 房屋租赁合同（修订）" {
		t.Fatalf("update lost title: %q", again.Title)
	}
}

// TestMigrationAddsContractSourceColumn simulates a database created before B7
// (schema version 7, no source column) and checks the upgrade keeps every row
// while adding the new column with an empty default.
func TestMigrationAddsContractSourceColumn(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	legacy := model.Contract{Title: "B7 之前的合同", Year: 2025, Data: map[string]any{"rent": 800.0}}
	if err := st.PutContract(ctx, &legacy); err != nil {
		t.Fatal(err)
	}
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_contracts_source`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE contracts DROP COLUMN source_contract_id`); err != nil {
		t.Fatalf("drop source column: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = 8`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	upgraded, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade v7 database: %v", err)
	}
	defer upgraded.Close()
	if v, _ := upgraded.SchemaVersion(); v != store.LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", v, store.LatestSchemaVersion())
	}
	reader := mustReader(t, upgraded)
	got, err := reader.GetContract(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "B7 之前的合同" || got.Year != 2025 || got.Data["rent"] != 800.0 {
		t.Fatalf("legacy contract changed by migration: %+v", got)
	}
	if got.SourceContractID != "" {
		t.Fatalf("new column default = %q, want empty", got.SourceContractID)
	}
	// New contracts written after the upgrade can carry a source link; the
	// legacy row keeps its empty provenance because updates never write it.
	fresh := model.Contract{Title: "升级后续签合同", Year: 2026, SourceContractID: legacy.ID}
	if err := upgraded.PutContract(ctx, &fresh); err != nil {
		t.Fatal(err)
	}
	reloaded, err := reader.GetContract(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SourceContractID != legacy.ID {
		t.Fatalf("post-upgrade source = %q", reloaded.SourceContractID)
	}
	legacyAgain, err := reader.GetContract(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyAgain.SourceContractID != "" {
		t.Fatalf("legacy row gained a source link: %q", legacyAgain.SourceContractID)
	}
}

// TestLatestSuccessfulDocumentUsesInsertionOrder pins the regenerate source to
// the most recently inserted document. created_at is second-precision, so the
// ids are chosen to make the old ORDER BY created_at DESC, id DESC pick the
// wrong row: only rowid ordering returns the second document.
func TestLatestSuccessfulDocumentUsesInsertionOrder(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	contract := model.Contract{Title: "B4 排序测试"}
	if err := st.PutContract(ctx, &contract); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	first := model.OutputDocument{ID: "doc_z", ContractID: contract.ID, FilePath: "outputs/first.pdf",
		SHA256: "a", Status: model.DocumentStatusSuccess, CreatedAt: at}
	second := model.OutputDocument{ID: "doc_a", ContractID: contract.ID, FilePath: "outputs/second.pdf",
		SHA256: "b", Status: model.DocumentStatusSuccess, CreatedAt: at}
	for _, d := range []*model.OutputDocument{&first, &second} {
		if err := st.PutOutputDocument(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	reader := mustReader(t, st)
	got, err := reader.LatestSuccessfulDocument(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != second.ID {
		t.Fatalf("latest = %s, want %s (insertion order; id order would pick %s)", got.ID, second.ID, first.ID)
	}
}
