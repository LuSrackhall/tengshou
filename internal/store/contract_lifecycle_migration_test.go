package store_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// TestMigrationRebuildsContractLifecycleTable pins the destructive part of the
// v0.2 lifecycle migration: adding the terminated status requires rebuilding
// contracts (SQLite cannot alter a CHECK constraint). The rebuild must keep
// every contract row, its column values and all child references (signatures,
// output documents, seal links).
func TestMigrationRebuildsContractLifecycleTable(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()

	partyA := model.Party{Kind: model.PartyOrganization, Name: "迁移甲方"}
	partyB := model.Party{Kind: model.PartyPerson, Name: "迁移乙方"}
	if err := st.PutParty(ctx, &partyA); err != nil {
		t.Fatal(err)
	}
	if err := st.PutParty(ctx, &partyB); err != nil {
		t.Fatal(err)
	}
	contract := model.Contract{
		Title: "生命周期迁移合同", PartyAID: partyA.ID, PartyBID: partyB.ID,
		Status: model.ContractActive, Year: 2026, Amount: 1200,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31",
		Data: map[string]any{"rent": 100.0},
	}
	if err := st.PutContract(ctx, &contract); err != nil {
		t.Fatal(err)
	}
	sig := model.Signature{ContractID: contract.ID, SignerName: "迁移乙方", AssetPath: "assets/sig.png", SHA256: "abc"}
	if err := st.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}
	doc := model.OutputDocument{ContractID: contract.ID, FilePath: "outputs/a.pdf", SHA256: "def"}
	if err := st.PutOutputDocument(ctx, &doc); err != nil {
		t.Fatal(err)
	}
	seal := model.Seal{Name: "迁移公章", AssetPath: "assets/seal.png", SHA256: "xyz", MIME: "image/png"}
	if err := st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachSeal(ctx, seal.ID, []string{contract.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	st.Close()

	// Rewind to schema version 11: drop the lifecycle columns and forget the
	// migration, leaving the pre-rebuild table and all child rows in place.
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"termination_note", "termination_reason", "terminated_on"} {
		if _, err := db.Exec(`ALTER TABLE contracts DROP COLUMN ` + column); err != nil {
			t.Fatalf("drop %s: %v", column, err)
		}
	}
	var sigContract, docContract, sealContracts string
	if err := db.QueryRow(`SELECT contract_id FROM signature_assets WHERE id = ?`, sig.ID).Scan(&sigContract); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT contract_id FROM output_documents WHERE id = ?`, doc.ID).Scan(&docContract); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contract_seals WHERE contract_id = ?`, contract.ID).Scan(&sealContracts); err != nil {
		t.Fatal(err)
	}
	if sigContract != contract.ID || docContract != contract.ID || sealContracts != "1" {
		t.Fatalf("child links before migration = %q %q %q", sigContract, docContract, sealContracts)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = 12`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("migrate lifecycle rebuild: %v", err)
	}
	defer reopened.Close()
	reader := mustReader(t, reopened)
	got, err := reader.GetContract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != contract.Title || got.Year != 2026 || got.AssetID != "" ||
		got.LeaseStart != "2026-01-01" || got.LeaseEnd != "2026-12-31" ||
		got.Amount != 1200 || got.Data["rent"] != 100.0 {
		t.Fatalf("contract changed by rebuild: %+v", got)
	}
	if got.TerminatedOn != "" || got.TerminationReason != "" || got.TerminationNote != "" {
		t.Fatalf("new lifecycle columns should default empty: %+v", got)
	}
	// Child references survived the DROP/rename.
	db2, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := db2.QueryRow(`SELECT contract_id FROM signature_assets WHERE id = ?`, sig.ID).Scan(&sigContract); err != nil {
		t.Fatal(err)
	}
	if err := db2.QueryRow(`SELECT contract_id FROM output_documents WHERE id = ?`, doc.ID).Scan(&docContract); err != nil {
		t.Fatal(err)
	}
	if err := db2.QueryRow(`SELECT COUNT(*) FROM contract_seals WHERE contract_id = ?`, contract.ID).Scan(&sealContracts); err != nil {
		t.Fatal(err)
	}
	if sigContract != contract.ID || docContract != contract.ID || sealContracts != "1" {
		t.Fatalf("child links after migration = %q %q %q", sigContract, docContract, sealContracts)
	}

	// The rebuilt CHECK accepts terminated and rejects unknown statuses.
	terminated := model.Contract{
		Title: "迁移后终止合同", PartyBID: partyB.ID, Status: model.ContractTerminated,
		TerminatedOn: "2026-06-30", TerminationReason: model.TerminationReasonEarly,
	}
	if err := reopened.PutContract(ctx, &terminated); err != nil {
		t.Fatalf("write terminated after rebuild: %v", err)
	}
	invalid := model.Contract{Title: "非法状态", PartyBID: partyB.ID, Status: "cancelled"}
	if err := reopened.PutContract(ctx, &invalid); err == nil {
		t.Fatal("invalid status should be rejected by the rebuilt CHECK")
	}

	// The built-in status field lists 已终止 for the form select.
	var options string
	if err := db2.QueryRow(
		`SELECT options_json FROM field_definitions WHERE entity = 'contract' AND key = 'status'`,
	).Scan(&options); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(options, "已终止") {
		t.Fatalf("status field options = %s", options)
	}
}
