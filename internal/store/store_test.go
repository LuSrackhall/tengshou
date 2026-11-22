package store_test

import (
	"context"
	"database/sql"
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
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}
	// Re-opening is idempotent.
	st2, err := store.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if v, _ := st2.SchemaVersion(); v != 2 {
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
	if v, _ := reopened.SchemaVersion(); v != 2 {
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
