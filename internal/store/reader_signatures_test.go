package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// TestContractDetailSignatureSelection pins the signature-link selection on a
// contract detail: a legacy party row (contract_id NULL, source ”) stays a
// fallback only, and a contract-linked row for the same party replaces it even
// when both rows share the same timestamp and the legacy id sorts first.
func TestContractDetailSignatureSelection(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	rd := mustReader(t, st)

	partyA := model.Party{Name: "签名选择甲方"}
	if err := st.PutParty(ctx, &partyA); err != nil {
		t.Fatal(err)
	}
	partyB := model.Party{Name: "签名选择乙方"}
	if err := st.PutParty(ctx, &partyB); err != nil {
		t.Fatal(err)
	}
	contract := model.Contract{Title: "签名选择合同", Year: 2026, PartyAID: partyA.ID, PartyBID: partyB.ID}
	if err := st.PutContract(ctx, &contract); err != nil {
		t.Fatal(err)
	}

	// Same capture instant everywhere so ordering must not decide anything;
	// legacy ids sort before the linked id under (captured_at, created_at, id).
	same := time.Date(2026, 11, 28, 3, 0, 0, 0, time.UTC)
	legacyA := model.Signature{
		ID: "sig-000-legacy-a", PartyID: partyA.ID, SignerName: partyA.Name,
		AssetPath: "assets/signatures/a-legacy.png", SHA256: "sha-legacy-a",
		CapturedAt: same, CreatedAt: same,
	}
	linkedA := model.Signature{
		ID: "sig-999-linked-a", PartyID: partyA.ID, ContractID: contract.ID,
		SignerName: partyA.Name, AssetPath: "assets/signatures/a-linked.png", SHA256: "sha-linked-a",
		Source: model.SignatureSourceArchive, CapturedAt: same, CreatedAt: same,
	}
	legacyB := model.Signature{
		ID: "sig-000-legacy-b", PartyID: partyB.ID, SignerName: partyB.Name,
		AssetPath: "assets/signatures/b-legacy.png", SHA256: "sha-legacy-b",
		CapturedAt: same, CreatedAt: same,
	}
	for _, sig := range []*model.Signature{&legacyA, &linkedA, &legacyB} {
		if err := st.PutSignature(ctx, sig); err != nil {
			t.Fatalf("put signature %s: %v", sig.ID, err)
		}
	}

	detail, err := rd.GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, sig := range detail.Signatures {
		got[sig.ID] = true
	}
	if !got[linkedA.ID] {
		t.Fatalf("contract-linked signature missing: %+v", detail.Signatures)
	}
	if got[legacyA.ID] {
		t.Fatalf("legacy row covered by a linked signature must be excluded: %+v", detail.Signatures)
	}
	if !got[legacyB.ID] {
		t.Fatalf("party without a linked signature must keep the legacy fallback: %+v", detail.Signatures)
	}

	// A contract with no linked signature at all still reads both legacy rows.
	other := model.Contract{Title: "无关联签名合同", Year: 2026, PartyAID: partyA.ID, PartyBID: partyB.ID}
	if err := st.PutContract(ctx, &other); err != nil {
		t.Fatal(err)
	}
	otherDetail, err := rd.GetContractDetail(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherIDs := map[string]bool{}
	for _, sig := range otherDetail.Signatures {
		otherIDs[sig.ID] = true
	}
	if !otherIDs[legacyA.ID] || !otherIDs[legacyB.ID] || otherIDs[linkedA.ID] {
		t.Fatalf("legacy fallback set = %+v", otherDetail.Signatures)
	}
}
