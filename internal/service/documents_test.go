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

// b4Template imports a one-page template with a signature placement, a seal
// placement and a contract-number field. It returns the saved version.
func b4Template(t *testing.T, f *templateFixture, sealID string) model.Template {
	t.Helper()
	imported := f.importTemplate(t, "B4 测试模板")
	saved, err := f.svc.SaveTemplateMapping(context.Background(), imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "no", Page: 1, X: 120, Y: 150, W: 200, H: 22, Type: model.PlacementField, Field: "contract.contract_no"},
			{ID: "sig", Page: 1, X: 300, Y: 600, W: 140, H: 60, Type: model.PlacementSignature, Party: model.PlacementPartyB},
			{ID: "seal", Page: 1, X: 120, Y: 600, W: 120, H: 120, Type: model.PlacementSeal,
				SealMode: model.PlacementSealFixed, SealID: sealID},
		},
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	return saved.Template
}

func b4Signature(t *testing.T, f *templateFixture, party model.Party, contentSHA string) model.Signature {
	t.Helper()
	rel, w, h := f.writePNG(t, "assets/signatures/"+party.ID+".png")
	sig := model.Signature{
		PartyID: party.ID, SignerName: party.Name, AssetPath: rel,
		SHA256: "sigsha-" + party.ID, ContentSHA256: contentSHA, WidthPx: w, HeightPx: h,
	}
	if err := f.st.PutSignature(context.Background(), &sig); err != nil {
		t.Fatalf("put signature: %v", err)
	}
	return sig
}

func mustErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var be *service.Error
	if !errors.As(err, &be) || be.Code != want {
		t.Fatalf("error = %v, want code %q", err, want)
	}
}

// TestArchiveSignatureReuseBypassesContentGate pins v0.2 D3=B: an archive
// signature reused into a contract stamps as-is. Later content edits must not
// resurrect the B5 content_changed gate, the generation record must name the
// archive source, and replacing the archive signature must leave both the
// linked signature row and the already-generated document snapshot untouched.
func TestArchiveSignatureReuseBypassesContentGate(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "档案复用租户")
	sealRel, _, _ := f.writePNG(t, "assets/seals/seal.png")
	seal := model.Seal{Name: "测试公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	tpl := b4Template(t, f, seal.ID)

	// The archive signature itself is captured once (source lan_qr, no
	// contract link); the party archive points at it.
	archiveRel, aw, ah := f.writePNG(t, "assets/signatures/archive.png")
	archive := model.Signature{
		PartyID: party.ID, SignerName: party.Name, AssetPath: archiveRel,
		SHA256: "archive-sha", WidthPx: aw, HeightPx: ah,
		Source: model.SignatureSourceLANQR,
	}
	if err := f.st.PutSignature(ctx, &archive); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AttachPartySignature(ctx, party.ID, archive.ID); err != nil {
		t.Fatal(err)
	}

	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "档案复用合同", PartyBID: party.ID, Amount: 1500,
	})
	reused, err := f.svc.ReusePartySignature(ctx, contract.ID, party.ID)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if reused.Source != model.SignatureSourceArchive || reused.ContentSHA256 != "" {
		t.Fatalf("reused signature = %+v", reused)
	}

	preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !preflight.Ready || preflight.Status != service.PreflightReady {
		t.Fatalf("preflight = %+v", preflight)
	}
	if len(preflight.Signatures) != 1 ||
		preflight.Signatures[0].Source != model.SignatureSourceArchive ||
		preflight.Signatures[0].ContentSHA256 != "" {
		t.Fatalf("signature snapshot = %+v", preflight.Signatures)
	}

	preview, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview.PreviewSHA256,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(first.Document.Signatures) != 1 ||
		first.Document.Signatures[0].SignatureID != reused.ID ||
		first.Document.Signatures[0].Source != model.SignatureSourceArchive {
		t.Fatalf("generation record signatures = %+v", first.Document.Signatures)
	}

	// Editing the contract content must not require re-confirming the archive
	// signature: generation stays ready and succeeds without allow_stale.
	if _, _, err := f.svc.SaveContract(ctx, service.ContractInput{
		ID: contract.ID, ContractNo: contract.ContractNo, Year: contract.Year,
		Title: "档案复用合同（改名）", PartyBID: party.ID, Amount: contract.Amount,
	}); err != nil {
		t.Fatalf("update contract: %v", err)
	}
	afterEdit, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight after edit: %v", err)
	}
	if !afterEdit.Ready || afterEdit.Status != service.PreflightReady {
		t.Fatalf("preflight after edit = %+v", afterEdit)
	}
	for _, issue := range afterEdit.Issues {
		if issue.Code == "content_changed" {
			t.Fatalf("archive reuse must not be judged against content version: %+v", afterEdit.Issues)
		}
	}
	preview2, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview2.PreviewSHA256,
	}); err != nil {
		t.Fatalf("generate after edit: %v", err)
	}

	// Replacing the archive signature only affects future reuse. The row
	// already linked to this contract and the generated document snapshot keep
	// the original asset, so an issued PDF never changes underneath the user.
	replacementRel, rw, rh := f.writePNG(t, "assets/signatures/archive2.png")
	replacement := model.Signature{
		PartyID: party.ID, SignerName: party.Name, AssetPath: replacementRel,
		SHA256: "archive-sha-2", WidthPx: rw, HeightPx: rh,
		Source: model.SignatureSourceLANQR,
	}
	if err := f.st.PutSignature(ctx, &replacement); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AttachPartySignature(ctx, party.ID, replacement.ID); err != nil {
		t.Fatal(err)
	}
	linked, err := f.rd.GetSignature(ctx, reused.ID)
	if err != nil {
		t.Fatalf("read linked signature: %v", err)
	}
	if linked.AssetPath != archive.AssetPath || linked.SHA256 != archive.SHA256 {
		t.Fatalf("archive replacement changed the linked signature: %+v", linked)
	}
	docs, err := f.rd.ListDocuments(ctx, model.ListFilter{
		ContractID: contract.ID, Status: model.DocumentStatusSuccess, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs.Items {
		for _, snap := range doc.Signatures {
			if snap.SignatureID == reused.ID && snap.SHA256 != archive.SHA256 {
				t.Fatalf("archive replacement changed an issued document snapshot: %+v", snap)
			}
		}
	}
	fresh, err := f.svc.ReusePartySignature(ctx, contract.ID, party.ID)
	if err != nil {
		t.Fatalf("reuse after archive replacement: %v", err)
	}
	if fresh.ID == reused.ID || fresh.AssetPath != replacement.AssetPath {
		t.Fatalf("new reuse should pick the replacement asset: %+v", fresh)
	}
}

// TestGenerateRecordsHistoryAndBlocksChangedContent verifies generation history,
// regenerate links, failed attempts and the B5 content gate for on-site
// signatures.
func TestGenerateRecordsHistoryAndBlocksChangedContent(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "张三")
	sealRel, _, _ := f.writePNG(t, "assets/seals/seal.png")
	seal := model.Seal{Name: "测试公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	tpl := b4Template(t, f, seal.ID)

	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "B4 房屋租赁合同", PartyBID: party.ID, Amount: 1200,
	})
	digest, err := service.ContentSHA256(contract)
	if err != nil {
		t.Fatal(err)
	}
	sig := b4Signature(t, f, party, digest)

	// Preflight reports readiness with the content version and the preview
	// digest the generate request must echo.
	preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !preflight.Ready || preflight.Status != service.PreflightReady {
		t.Fatalf("preflight = %+v", preflight)
	}
	if preflight.ContentSHA256 != digest || len(preflight.PreviewSHA256) != 64 {
		t.Fatalf("digests = %q %q", preflight.ContentSHA256, preflight.PreviewSHA256)
	}
	if len(preflight.Signatures) != 1 || preflight.Signatures[0].SignatureID != sig.ID {
		t.Fatalf("signature snapshot = %+v", preflight.Signatures)
	}

	preview, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	first, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview.PreviewSHA256,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	doc := first.Document
	if doc.Status != model.DocumentStatusSuccess || doc.Kind != model.DocumentKindInitial {
		t.Fatalf("document = %+v", doc)
	}
	if doc.Operator != "测试员" || doc.ContentSHA256 != digest || doc.TemplateVersion != tpl.Version {
		t.Fatalf("document trace = %+v", doc)
	}
	if len(doc.Signatures) != 1 || doc.Signatures[0].SignatureID != sig.ID ||
		doc.Signatures[0].ContentSHA256 != digest {
		t.Fatalf("signature snapshot = %+v", doc.Signatures)
	}

	// A second output without content changes is recorded as a regenerate and
	// points at the previous document.
	preview2, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview2.PreviewSHA256,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if second.Document.Kind != model.DocumentKindRegenerate || second.Document.SourceDocumentID != doc.ID {
		t.Fatalf("regenerate document = %+v", second.Document)
	}

	// Changing the contract content invalidates the retained signature.
	changed := contract
	changed.Title = "B4 房屋租赁合同（改名）"
	if _, _, err := f.svc.SaveContract(ctx, service.ContractInput{
		ID: changed.ID, ContractNo: changed.ContractNo, Year: changed.Year,
		Title: changed.Title, PartyBID: changed.PartyBID, Amount: changed.Amount,
		Status: model.ContractActive,
	}); err != nil {
		t.Fatalf("update contract: %v", err)
	}
	preview3, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview3.PreviewSHA256,
	})
	mustErrorCode(t, err, "content_changed")

	failed, err := f.rd.ListDocuments(ctx, model.ListFilter{
		ContractID: contract.ID, Status: model.DocumentStatusFailed, Limit: 10,
	})
	if err != nil || failed.Total == 0 {
		t.Fatalf("failed records = %+v err=%v", failed, err)
	}
	if !strings.Contains(failed.Items[0].ErrorMessage, "签名") {
		t.Fatalf("failure message = %q", failed.Items[0].ErrorMessage)
	}

	// Explicit confirmation reuses the stale signature and audits the action.
	preview4, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview4.PreviewSHA256,
		AllowStaleSignature: true,
	})
	if err != nil {
		t.Fatalf("confirmed regenerate: %v", err)
	}
	if confirmed.Document.ContentSHA256 == digest {
		t.Fatalf("content digest did not change: %q", confirmed.Document.ContentSHA256)
	}
	logs, err := f.rd.ListChangeLogs(ctx, store.ChangeFilter{EntityID: contract.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	foundAudit := false
	for _, entry := range logs.Items {
		if strings.Contains(entry.Summary, "确认保留旧签名重出") {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatal("missing stale-signature confirmation audit entry")
	}

	// A contract whose signer has no signature is blocked instead of silently
	// omitting it.
	unsignedParty := mustParty(t, f.svc, "王五")
	unsigned := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "B4 无签名合同", PartyBID: unsignedParty.ID, Amount: 100,
	})
	previewUnsigned, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: unsigned.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: unsigned.ID, PreviewSHA256: previewUnsigned.PreviewSHA256,
	})
	mustErrorCode(t, err, "missing_signature")

	// A required field added later becomes a generation gate too.
	if _, err := f.svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "filing_no", Label: "备案号",
		Type: model.FieldTypeText, Required: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	unsignedPreflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, unsigned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unsignedPreflight.Ready || unsignedPreflight.Status != service.PreflightNeedsSignature {
		t.Fatalf("preflight after required field = %+v", unsignedPreflight)
	}
	codes := map[string]bool{}
	for _, issue := range unsignedPreflight.Issues {
		codes[issue.Code] = true
	}
	if !codes["missing_field"] || !codes["missing_signature"] {
		t.Fatalf("issues = %+v", unsignedPreflight.Issues)
	}
}

func TestBatchGenerationContinuesPastFailures(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "李四")
	staleParty := mustParty(t, f.svc, "孙七")
	unsignedParty := mustParty(t, f.svc, "赵六")
	sealRel, _, _ := f.writePNG(t, "assets/seals/batch-seal.png")
	seal := model.Seal{Name: "批量公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	tpl := b4Template(t, f, seal.ID)

	ready := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "批量合同甲", PartyBID: party.ID, Amount: 100,
	})
	stale := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "批量合同乙", PartyBID: staleParty.ID, Amount: 200,
	})
	unsigned := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "批量合同丙", PartyBID: unsignedParty.ID, Amount: 300,
	})
	digest, err := service.ContentSHA256(ready)
	if err != nil {
		t.Fatal(err)
	}
	b4Signature(t, f, party, digest)
	b4Signature(t, f, staleParty, "")

	preview, err := f.svc.PreflightBatchGeneration(ctx, service.BatchPreviewInput{
		TemplateID: tpl.ID, ContractIDs: []string{ready.ID, stale.ID, unsigned.ID}, Sample: true,
	})
	if err != nil {
		t.Fatalf("batch preview: %v", err)
	}
	if len(preview.Items) != 3 || preview.Summary.Total != 3 ||
		preview.Summary.Ready != 2 || preview.Summary.Blocked != 1 {
		t.Fatalf("batch preview summary = %+v items=%d", preview.Summary, len(preview.Items))
	}
	if len(preview.SamplePDF) == 0 || len(preview.SamplePreviewSHA256) != 64 {
		t.Fatalf("sample preview missing")
	}
	byID := map[string]service.PreflightResult{}
	for _, item := range preview.Items {
		byID[item.ContractID] = item
	}
	if !byID[ready.ID].Ready || !byID[stale.ID].Ready || byID[unsigned.ID].Ready {
		t.Fatalf("item readiness = %+v", preview.Items)
	}
	if byID[unsigned.ID].Status != service.PreflightNeedsSignature {
		t.Fatalf("unsigned status = %q", byID[unsigned.ID].Status)
	}

	// The second item echoes a stale digest, so it fails; the third has no
	// signature and is blocked. Neither failure stops the batch and the first
	// item still succeeds.
	result, err := f.svc.GenerateContractBatch(ctx, service.BatchGenerateInput{
		TemplateID: tpl.ID,
		Items: []service.BatchGenerateItemInput{
			{ContractID: ready.ID, PreviewSHA256: byID[ready.ID].PreviewSHA256},
			{ContractID: stale.ID, PreviewSHA256: "deadbeef"},
			{ContractID: unsigned.ID, PreviewSHA256: byID[unsigned.ID].PreviewSHA256},
		},
	})
	if err != nil {
		t.Fatalf("batch generate: %v", err)
	}
	if result.Summary.Total != 3 || result.Summary.Success != 1 || result.Summary.Failed != 2 {
		t.Fatalf("batch summary = %+v", result.Summary)
	}
	for _, item := range result.Items {
		switch item.ContractID {
		case ready.ID:
			if !item.Success || item.Document == nil || item.Document.BatchID != result.BatchID ||
				item.Document.Status != model.DocumentStatusSuccess {
				t.Fatalf("ready item = %+v", item)
			}
		case stale.ID:
			if item.Success || item.ErrorCode != "preview_required" {
				t.Fatalf("stale item = %+v", item)
			}
		case unsigned.ID:
			if item.Success || item.ErrorCode != "missing_signature" {
				t.Fatalf("unsigned item = %+v", item)
			}
		}
	}
	failed, err := f.rd.ListDocuments(ctx, model.ListFilter{BatchID: result.BatchID, Status: model.DocumentStatusFailed})
	if err != nil || failed.Total != 1 {
		t.Fatalf("failed batch records = %+v err=%v", failed, err)
	}

	// F-TE-7：批量生成的每个成功项都写入合约 generate 变更记录（B9 审计要求）。
	audit, err := f.rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityContract, EntityID: ready.ID, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	generated := false
	for _, entry := range audit.Items {
		if entry.Action == model.ChangeActionGenerate && strings.Contains(entry.Summary, "生成 PDF") {
			generated = true
			break
		}
	}
	if !generated {
		t.Fatalf("batch generation audit missing: %+v", audit.Items)
	}

	// A single-contract retry after a fresh preview succeeds and is recorded in
	// the same contract history.
	retryPreview, err := f.svc.PreflightBatchGeneration(ctx, service.BatchPreviewInput{
		TemplateID: tpl.ID, ContractIDs: []string{stale.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := f.svc.GenerateContractBatch(ctx, service.BatchGenerateInput{
		TemplateID: tpl.ID,
		Items: []service.BatchGenerateItemInput{
			{ContractID: stale.ID, PreviewSHA256: retryPreview.Items[0].PreviewSHA256},
		},
	})
	if err != nil || retry.Summary.Success != 1 {
		t.Fatalf("retry = %+v err=%v", retry, err)
	}
	docs, err := f.rd.ListDocuments(ctx, model.ListFilter{ContractID: stale.ID, Status: model.DocumentStatusSuccess})
	if err != nil || docs.Total != 1 {
		t.Fatalf("retried documents = %+v err=%v", docs, err)
	}
}

// TestRegenerateAfterTemplateVersionChangeKeepsSignature locks the headline
// acceptance rule: after a template version change, regenerating unchanged
// content keeps the existing signature, while a later content change blocks
// regeneration on the new version too.
func TestRegenerateAfterTemplateVersionChangeKeepsSignature(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "周九")
	sealRel, _, _ := f.writePNG(t, "assets/seals/regenerate-seal.png")
	seal := model.Seal{Name: "换版公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}
	tpl := b4Template(t, f, seal.ID)

	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "B4 换版重出合同", PartyBID: party.ID, Amount: 900,
	})
	digest, err := service.ContentSHA256(contract)
	if err != nil {
		t.Fatal(err)
	}
	sig := b4Signature(t, f, party, digest)

	preview, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	first, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preview.PreviewSHA256,
	})
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	firstDoc := first.Document
	if firstDoc.Kind != model.DocumentKindInitial || firstDoc.TemplateID != tpl.ID {
		t.Fatalf("initial document = %+v", firstDoc)
	}

	// Saving the mapping again creates a new template version while the
	// contract content fingerprint stays unchanged.
	nextPlacements := append([]model.Placement(nil), tpl.Placements...)
	moved := false
	for i := range nextPlacements {
		if nextPlacements[i].ID == "no" {
			nextPlacements[i].Y++
			moved = true
		}
	}
	if !moved {
		t.Fatal("template placements missing the contract_no field")
	}
	saved, err := f.svc.SaveTemplateMapping(ctx, tpl.ID, service.TemplateSaveInput{Placements: nextPlacements})
	if err != nil {
		t.Fatalf("save new template version: %v", err)
	}
	next := saved.Template
	if next.ID == tpl.ID || next.Version <= tpl.Version {
		t.Fatalf("new template version = %+v (previous %s v%d)", next, tpl.ID, tpl.Version)
	}

	// The new version reuses the signature because the content did not change.
	preflight, err := f.svc.PreflightContractGeneration(ctx, next.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight new version: %v", err)
	}
	if !preflight.Ready || preflight.Status != service.PreflightReady {
		t.Fatalf("preflight new version = %+v", preflight)
	}
	if preflight.ContentSHA256 != digest || preflight.TemplateVersion != next.Version {
		t.Fatalf("preflight digests/version = %+v", preflight)
	}
	for _, issue := range preflight.Issues {
		if issue.Code == "content_changed" {
			t.Fatalf("unexpected content_changed issue: %+v", preflight.Issues)
		}
	}
	if len(preflight.Signatures) != 1 || preflight.Signatures[0].SignatureID != sig.ID ||
		preflight.Signatures[0].ContentSHA256 != digest {
		t.Fatalf("preflight signature snapshot = %+v", preflight.Signatures)
	}

	previewNext, err := f.svc.PreviewTemplate(ctx, next.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("preview new version: %v", err)
	}
	second, err := f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: next.ID, ContractID: contract.ID, PreviewSHA256: previewNext.PreviewSHA256,
	})
	if err != nil {
		t.Fatalf("regenerate on new version: %v", err)
	}
	secondDoc := second.Document
	if secondDoc.Kind != model.DocumentKindRegenerate || secondDoc.SourceDocumentID != firstDoc.ID {
		t.Fatalf("regenerate document = %+v", secondDoc)
	}
	if secondDoc.TemplateID != next.ID || secondDoc.TemplateVersion != next.Version {
		t.Fatalf("regenerate template = %+v", secondDoc)
	}
	if secondDoc.ContentSHA256 != digest || len(secondDoc.Signatures) != 1 ||
		secondDoc.Signatures[0].SignatureID != sig.ID ||
		secondDoc.Signatures[0].ContentSHA256 != digest {
		t.Fatalf("regenerate trace = %+v", secondDoc)
	}

	// A later content change blocks the new version exactly like the old one.
	changed := contract
	changed.Title = "B4 换版重出合同（改名）"
	if _, _, err := f.svc.SaveContract(ctx, service.ContractInput{
		ID: changed.ID, ContractNo: changed.ContractNo, Year: changed.Year,
		Title: changed.Title, PartyBID: changed.PartyBID, Amount: changed.Amount,
	}); err != nil {
		t.Fatalf("update contract: %v", err)
	}
	changedPreflight, err := f.svc.PreflightContractGeneration(ctx, next.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight changed content: %v", err)
	}
	if changedPreflight.Ready || changedPreflight.Status != service.PreflightNeedsResign {
		t.Fatalf("changed preflight = %+v", changedPreflight)
	}
	previewChanged, err := f.svc.PreviewTemplate(ctx, next.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("preview changed content: %v", err)
	}
	_, err = f.svc.GenerateContractDocument(ctx, service.GenerateInput{
		TemplateID: next.ID, ContractID: contract.ID, PreviewSHA256: previewChanged.PreviewSHA256,
	})
	mustErrorCode(t, err, "content_changed")
}

// TestPreflightGatesMissingPartyAndSeal covers the two output gates that had
// no direct tests: a contract whose counterparty archive disappeared and a
// seal whose image file is gone.
func TestPreflightGatesMissingPartyAndSeal(t *testing.T) {
	t.Run("missing_party", func(t *testing.T) {
		f := newTemplateFixture(t)
		ctx := context.Background()
		party := mustParty(t, f.svc, "缺失相对方")
		sealRel, _, _ := f.writePNG(t, "assets/seals/party-seal.png")
		seal := model.Seal{Name: "相对方公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
		if err := f.st.PutSeal(ctx, &seal); err != nil {
			t.Fatal(err)
		}
		// No signature placement so this case isolates the counterparty gate.
		imported := f.importTemplate(t, "B4 缺相对方模板")
		saved, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
			Placements: []model.Placement{
				{ID: "no", Page: 1, X: 120, Y: 150, W: 200, H: 22, Type: model.PlacementField, Field: "contract.contract_no"},
				{ID: "seal", Page: 1, X: 120, Y: 600, W: 120, H: 120, Type: model.PlacementSeal,
					SealMode: model.PlacementSealFixed, SealID: seal.ID},
			},
		})
		if err != nil {
			t.Fatalf("save mapping: %v", err)
		}
		tpl := saved.Template
		contract := mustContract(t, f.svc, service.ContractInput{
			Year: 2026, Title: "B4 缺相对方合同", PartyBID: party.ID, Amount: 100,
		})
		// Simulate a contract whose counterparty archive disappeared; the
		// preflight gate must block instead of rendering without a party.
		if err := f.st.DeleteParty(ctx, party.ID); err != nil {
			t.Fatalf("delete party: %v", err)
		}
		preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if preflight.Ready || preflight.Status != service.PreflightMissingParty {
			t.Fatalf("preflight = %+v", preflight)
		}
		codes := map[string]bool{}
		for _, issue := range preflight.Issues {
			codes[issue.Code] = true
		}
		if !codes["missing_party"] {
			t.Fatalf("issues = %+v", preflight.Issues)
		}
		_, err = f.svc.GenerateContractDocument(ctx, service.GenerateInput{
			TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preflight.PreviewSHA256,
		})
		mustErrorCode(t, err, "missing_party")
	})

	t.Run("missing_seal", func(t *testing.T) {
		f := newTemplateFixture(t)
		ctx := context.Background()
		party := mustParty(t, f.svc, "缺公章相对方")
		// The seal record exists but its image file is gone.
		seal := model.Seal{Name: "缺失公章", AssetPath: "assets/seals/missing.png", SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
		if err := f.st.PutSeal(ctx, &seal); err != nil {
			t.Fatal(err)
		}
		tpl := b4Template(t, f, seal.ID)
		contract := mustContract(t, f.svc, service.ContractInput{
			Year: 2026, Title: "B4 缺公章合同", PartyBID: party.ID, Amount: 100,
		})
		digest, err := service.ContentSHA256(contract)
		if err != nil {
			t.Fatal(err)
		}
		b4Signature(t, f, party, digest)
		preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if preflight.Ready || preflight.Status != service.PreflightMissingSeal {
			t.Fatalf("preflight = %+v", preflight)
		}
		codes := map[string]bool{}
		for _, issue := range preflight.Issues {
			codes[issue.Code] = true
		}
		if !codes["missing_seal"] {
			t.Fatalf("issues = %+v", preflight.Issues)
		}
		_, err = f.svc.GenerateContractDocument(ctx, service.GenerateInput{
			TemplateID: tpl.ID, ContractID: contract.ID, PreviewSHA256: preflight.PreviewSHA256,
		})
		mustErrorCode(t, err, "missing_seal")
	})
}
