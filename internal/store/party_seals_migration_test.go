package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// TestMigrationAddsPartySealColumns simulates a database created before the
// party archive seal migration (schema version 14, no seal columns) and checks
// existing party rows default the new columns to empty and keep working after
// the upgrade. It also pins idempotency: opening the upgraded database again
// must not re-run or fail the additive migration.
func TestMigrationAddsPartySealColumns(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	legacy := model.Party{
		Kind: model.PartyOrganization, Name: "旧版企业租户", IDNumber: "91330100LEGACY15",
	}
	if err := st.PutParty(ctx, &legacy); err != nil {
		t.Fatal(err)
	}
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{
		"seal_asset_id",
		"seal_source",
		"signature_seal_asset_id",
		"signature_seal_source",
	} {
		if _, err := db.Exec(`ALTER TABLE parties DROP COLUMN ` + column); err != nil {
			t.Fatalf("drop %s: %v", column, err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = 15`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	upgraded, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade v14 database: %v", err)
	}
	defer upgraded.Close()
	if v, _ := upgraded.SchemaVersion(); v != store.LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", v, store.LatestSchemaVersion())
	}
	reader := mustReader(t, upgraded)
	got, err := reader.GetParty(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SealAssetID != "" || got.SealSource != "" ||
		got.SignatureSealID != "" || got.SignatureSealSource != "" {
		t.Fatalf("legacy party should default the seal columns to empty: %+v", got)
	}
	got.SealAssetID = "seal_migration_15"
	got.SealSource = model.PartyAssetSourceAppUpload
	got.SignatureSealID = "sig_migration_15"
	got.SignatureSealSource = model.PartyAssetSourceLANEnroll
	if err := upgraded.PutParty(ctx, &got); err != nil {
		t.Fatal(err)
	}
	again, err := reader.GetParty(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.SealAssetID != got.SealAssetID ||
		again.SealSource != model.PartyAssetSourceAppUpload ||
		again.SignatureSealID != got.SignatureSealID ||
		again.SignatureSealSource != model.PartyAssetSourceLANEnroll {
		t.Fatalf("seal columns did not persist after the upgrade: %+v", again)
	}

	// Reopening an already migrated database keeps it usable and idempotent.
	upgraded.Close()
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer reopened.Close()
	reopenedReader := mustReader(t, reopened)
	reloaded, err := reopenedReader.GetParty(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SealAssetID != got.SealAssetID || reloaded.SignatureSealID != got.SignatureSealID {
		t.Fatalf("seal columns lost after reopen: %+v", reloaded)
	}
}
