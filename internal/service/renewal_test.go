package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func businessCode(t *testing.T, err error) string {
	t.Helper()
	var be *service.Error
	if !errors.As(err, &be) {
		t.Fatalf("error = %v, want business error", err)
	}
	return be.Code
}

// TestRenewalDraftPrefillRules pins the B7 draft contract: contract content is
// carried over, identity/dates are cleared, and the dynamic data object is
// copied so repeat signings do not retype anything.
func TestRenewalDraftPrefillRules(t *testing.T) {
	svc, rd, st := openService(t)
	ctx := context.Background()
	partyA := mustParty(t, svc, "幸福村合作社")
	partyB := mustParty(t, svc, "张三")
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "area_sqm", Label: "平方数",
		Type: model.FieldTypeNumber, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	tpl := model.Template{Name: "续签模板", Version: 1, HTML: "<p>x</p>", IsActive: true}
	if err := st.PutTemplate(ctx, &tpl); err != nil {
		t.Fatal(err)
	}
	source := mustContract(t, svc, service.ContractInput{
		Year: 2025, Title: "2025 房屋租赁合同", Type: "租赁",
		PartyAID: partyA.ID, PartyBID: partyB.ID, TemplateID: tpl.ID,
		Amount: 1200, Status: model.ContractActive, SignedOn: "2025-01-05",
		ValidFrom: "2025-01-10", ValidTo: "2025-12-31",
		Data: map[string]any{"area_sqm": "88.5"},
	})

	draft, err := svc.RenewalDraft(ctx, source.ID)
	if err != nil {
		t.Fatalf("renewal draft: %v", err)
	}
	if draft.Source.ID != source.ID || draft.Source.ContractNo != source.ContractNo ||
		draft.Source.Title != source.Title || draft.Source.Year != source.Year {
		t.Fatalf("source summary = %+v", draft.Source)
	}
	in := draft.Input
	if in.ContractNo != "" {
		t.Fatalf("draft contract number = %q, want empty (auto-allocated)", in.ContractNo)
	}
	if in.Year != svc.Now().Year() {
		t.Fatalf("draft year = %d, want current year %d", in.Year, svc.Now().Year())
	}
	if in.Title != source.Title || in.Type != source.Type || in.Amount != source.Amount ||
		in.PartyAID != partyA.ID || in.PartyBID != partyB.ID || in.TemplateID != tpl.ID {
		t.Fatalf("copied fields = %+v", in)
	}
	if in.SignedOn != "" || in.ValidFrom != "" || in.ValidTo != "" {
		t.Fatalf("draft dates should be cleared: %+v", in)
	}
	if in.Status != model.ContractDraft {
		t.Fatalf("draft status = %q, want draft", in.Status)
	}
	if in.SourceContractID != source.ID {
		t.Fatalf("draft source = %q, want %q", in.SourceContractID, source.ID)
	}
	if in.Data["area_sqm"] != 88.5 {
		t.Fatalf("draft data = %#v", in.Data)
	}
	for _, key := range []string{"title", "type", "party_a_id", "party_b_id", "template_id", "amount", "area_sqm"} {
		if !containsString(draft.Reused, key) {
			t.Fatalf("reused = %v, missing %q", draft.Reused, key)
		}
	}

	// Saving the draft creates a normal draft contract with provenance, and
	// none of the source signatures are copied.
	renewed := mustContract(t, svc, draft.Input)
	if renewed.ID == source.ID {
		t.Fatal("renewal reused the source contract id")
	}
	if renewed.SourceContractID != source.ID {
		t.Fatalf("saved source = %q, want %q", renewed.SourceContractID, source.ID)
	}
	if renewed.ContractNo == "" || renewed.ContractNo == source.ContractNo {
		t.Fatalf("renewal contract number = %q", renewed.ContractNo)
	}
	if renewed.Status != model.ContractDraft || renewed.SignedOn != "" {
		t.Fatalf("renewal should start as an unsigned draft: %+v", renewed)
	}
	detail, err := rd.GetContractDetail(ctx, renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Signatures) != 0 {
		t.Fatalf("renewal copied signatures: %+v", detail.Signatures)
	}

	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityContract, EntityID: renewed.ID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "续签（来源 " + source.ContractNo + "）："
	if len(logs.Items) == 0 || !strings.HasPrefix(logs.Items[0].Summary, wantPrefix) {
		t.Fatalf("change log = %+v, want summary prefix %q", logs.Items, wantPrefix)
	}

	// Updates ignore the provenance column, so a caller cannot rewrite the
	// renewal chain through a normal edit.
	other := mustContract(t, svc, service.ContractInput{
		Year: 2025, Title: "另一份合同", PartyBID: partyB.ID,
	})
	updated, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: renewed.ID, Title: renewed.Title, PartyBID: partyB.ID,
		Amount: renewed.Amount, SourceContractID: other.ID,
		Data: map[string]any{"area_sqm": "88.5"},
	})
	if err != nil {
		t.Fatalf("update renewal: %v", err)
	}
	if updated.SourceContractID != source.ID {
		t.Fatalf("update changed source to %q, want %q", updated.SourceContractID, source.ID)
	}
}

func TestRenewalSourceValidation(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")

	if _, err := svc.RenewalDraft(ctx, "ctr_missing"); businessCode(t, err) != "not_found" {
		t.Fatalf("draft of missing contract error = %v", err)
	}
	_, _, err := svc.SaveContract(ctx, service.ContractInput{
		Title: "续签坏来源", PartyBID: party.ID, SourceContractID: "ctr_missing",
	})
	if code := businessCode(t, err); code != "not_found" {
		t.Fatalf("create with missing source code = %q", code)
	}
}

// TestRenewalSourceDoesNotAffectContentDigest pins that B7 provenance is pure
// metadata: the renewal link must not enter the content digest that signatures
// bind to. Without this guard a future refactor folding SourceContractID into
// ContentSHA256 would silently mark every existing renewal signature as
// content_changed, and no other test would fail.
func TestRenewalSourceDoesNotAffectContentDigest(t *testing.T) {
	base := model.Contract{
		ID: "ctr_base", ContractNo: "HT-2025-0007", Year: 2025,
		Title: "2025 房屋租赁合同", Type: "租赁",
		PartyAID: "pty_a", PartyBID: "pty_b",
		Status: model.ContractActive, SignedOn: "2025-01-05",
		ValidFrom: "2025-01-10", ValidTo: "2025-12-31",
		Amount: 1200, Data: map[string]any{"area_sqm": 88.5},
	}

	withoutSource, err := service.ContentSHA256(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"ctr_source_a", "ctr_source_b"} {
		withSource := base
		withSource.SourceContractID = sourceID
		digest, err := service.ContentSHA256(withSource)
		if err != nil {
			t.Fatal(err)
		}
		if digest != withoutSource {
			t.Fatalf("source %q changed the digest: got %s, want %s",
				sourceID, digest, withoutSource)
		}
	}

	// Controls: real content changes must still move the digest, so the
	// assertions above cannot pass by accident.
	changedTitle := base
	changedTitle.Title += "（续）"
	titleDigest, err := service.ContentSHA256(changedTitle)
	if err != nil {
		t.Fatal(err)
	}
	if titleDigest == withoutSource {
		t.Fatal("digest did not change when the title changed")
	}
	changedData := base
	changedData.Data = map[string]any{"area_sqm": 90}
	dataDigest, err := service.ContentSHA256(changedData)
	if err != nil {
		t.Fatal(err)
	}
	if dataDigest == withoutSource {
		t.Fatal("digest did not change when the data changed")
	}
}

// TestRenewalRequiresFreshSignature is the B7 security regression: a renewal
// must not inherit the source contract's retained signature, and generating a
// PDF for the new content needs a new signature (B4 preflight).
func TestRenewalRequiresFreshSignature(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "张三")
	sealRel, _, _ := f.writePNG(t, "assets/seals/seal.png")
	seal := model.Seal{Name: "测试公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	tpl := b4Template(t, f, seal.ID)

	source := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "B7 房屋租赁合同", PartyBID: party.ID, Amount: 1200,
	})
	digest, err := service.ContentSHA256(source)
	if err != nil {
		t.Fatal(err)
	}
	// The signature belongs to the source contract content version.
	sigRel, w, h := f.writePNG(t, "assets/signatures/source.png")
	sig := model.Signature{
		PartyID: party.ID, ContractID: source.ID, SignerName: party.Name,
		AssetPath: sigRel, SHA256: "source-sig-sha", ContentSHA256: digest,
		Source: model.SignatureSourceLANQR, WidthPx: w, HeightPx: h,
	}
	if err := f.st.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}

	draft, err := f.svc.RenewalDraft(ctx, source.ID)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	renewed := mustContract(t, f.svc, draft.Input)

	detail, err := f.rd.GetContractDetail(ctx, renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range detail.Signatures {
		if got.ID == sig.ID {
			t.Fatalf("renewal inherited source signature %s", sig.ID)
		}
	}
	if len(detail.Signatures) != 0 {
		t.Fatalf("renewal signatures = %+v, want empty", detail.Signatures)
	}

	preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, renewed.ID)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if preflight.Ready || preflight.Status != service.PreflightNeedsSignature {
		t.Fatalf("preflight = status %q ready=%v issues=%+v",
			preflight.Status, preflight.Ready, preflight.Issues)
	}
	for _, issue := range preflight.Issues {
		if issue.Code == "content_changed" {
			t.Fatalf("renewal should not be judged against the old signature: %+v", preflight.Issues)
		}
	}
}
