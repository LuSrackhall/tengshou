package service_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func sealPNG(t *testing.T, size, radius int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := (x-cx)*(x-cx) + (y-cy)*(y-cy)
			if d <= radius*radius && d >= (radius-8)*(radius-8) {
				img.SetNRGBA(x, y, color.NRGBA{190, 30, 30, 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSealImportAttachReplaceDelete(t *testing.T) {
	svc, rd, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()

	partyA, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyOrganization, Name: "幸福村股份经济合作社",
	})
	if err != nil {
		t.Fatal(err)
	}
	partyB := mustParty(t, svc, "张三")
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "房屋租赁合同", PartyAID: partyA.ID, PartyBID: partyB.ID,
	})
	contract2 := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "土地租赁合同", PartyAID: partyA.ID, PartyBID: partyB.ID,
	})

	seal, err := svc.ImportSeal(ctx, service.SealInput{
		Name: "合作社公章", PartyID: partyA.ID, Note: "测试图片", Image: sealPNG(t, 240, 90),
	})
	if err != nil {
		t.Fatalf("import seal: %v", err)
	}
	if seal.AssetPath == "" || len(seal.SHA256) != 64 || seal.PartyID != partyA.ID {
		t.Fatalf("seal = %+v", seal)
	}
	if !strings.HasPrefix(seal.AssetPath, "assets/seals/") || !strings.HasSuffix(seal.AssetPath, ".png") {
		t.Fatalf("asset path = %q", seal.AssetPath)
	}
	full := filepath.Join(dir, filepath.FromSlash(seal.AssetPath))
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("seal file missing: %v", err)
	}
	if seal.WidthPx >= 240 || seal.HeightPx >= 240 {
		t.Fatalf("expected trimmed dimensions, got %dx%d", seal.WidthPx, seal.HeightPx)
	}

	// A party seal is applicable to the party's contracts, and attaching it
	// to contracts links it explicitly (single and batch).
	all, attached, err := svc.SealsForContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || len(attached) != 0 || all[0].ID != seal.ID {
		t.Fatalf("party seals all=%+v attached=%+v", all, attached)
	}
	n, err := svc.AttachSealToContracts(ctx, seal.ID, []string{contract.ID, contract2.ID})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if n != 2 {
		t.Fatalf("attached = %d, want 2", n)
	}
	n, err = svc.AttachSealToContracts(ctx, seal.ID, []string{contract.ID})
	if err != nil {
		t.Fatalf("re-attach: %v", err)
	}
	if n != 0 {
		t.Fatalf("re-attach should be idempotent, got %d", n)
	}
	detail, err := rd.GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Seals) != 1 || detail.Seals[0].ID != seal.ID || detail.Seals[0].ContractCount != 2 {
		t.Fatalf("contract detail seals = %+v", detail.Seals)
	}
	all, attached, err = svc.SealsForContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || len(attached) != 1 {
		t.Fatalf("after attach all=%d attached=%d", len(all), len(attached))
	}
	contractLogs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityContract, EntityID: contract.ID, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundAttach := false
	for _, entry := range contractLogs.Items {
		if strings.Contains(entry.Summary, "关联公章图片") {
			foundAttach = true
		}
	}
	if !foundAttach {
		t.Fatalf("missing attach audit entry: %+v", contractLogs.Items)
	}

	// Replacing the picture changes the digest and file, keeps contract links,
	// and leaves an audit entry with both digests.
	oldSHA := seal.SHA256
	replaced, err := svc.ReplaceSealImage(ctx, seal.ID, sealPNG(t, 260, 110))
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if replaced.SHA256 == oldSHA {
		t.Fatal("replace should change the digest")
	}
	if replaced.AssetPath == seal.AssetPath {
		t.Fatal("replace should use a new digest-scoped file name")
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("old seal file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(replaced.AssetPath))); err != nil {
		t.Fatalf("new seal file missing: %v", err)
	}
	ids, err := rd.SealContractIDs(ctx, seal.ID)
	if err != nil || len(ids) != 2 {
		t.Fatalf("contract links after replace = %v err=%v", ids, err)
	}
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntitySeal, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	foundReplace := false
	for _, entry := range logs.Items {
		if entry.EntityID == seal.ID && strings.Contains(entry.Summary, "替换公章图片") {
			foundReplace = true
		}
	}
	if !foundReplace {
		t.Fatalf("missing replace audit entry: %+v", logs.Items)
	}

	// Detach one contract, then delete the seal; links are removed with it.
	removed, err := svc.DetachSealFromContracts(ctx, seal.ID, []string{contract2.ID})
	if err != nil || removed != 1 {
		t.Fatalf("detach = %d err=%v", removed, err)
	}
	if err := svc.DeleteSeal(ctx, seal.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := rd.GetSeal(ctx, seal.ID); err == nil {
		t.Fatal("deleted seal still readable")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(replaced.AssetPath))); !os.IsNotExist(err) {
		t.Fatalf("seal file should be removed, stat err=%v", err)
	}
	ids, err = rd.SealContractIDs(ctx, seal.ID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("links after delete = %v err=%v", ids, err)
	}
	detail, err = rd.GetContractDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Seals) != 0 {
		t.Fatalf("contract still lists deleted seal: %+v", detail.Seals)
	}
	sealLogs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntitySeal, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	foundDelete := false
	for _, entry := range sealLogs.Items {
		if entry.EntityID == seal.ID && entry.Action == model.ChangeActionDelete &&
			strings.Contains(entry.Summary, "删除公章图片") {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Fatalf("missing delete audit entry: %+v", sealLogs.Items)
	}
}

func TestSealValidation(t *testing.T) {
	svc, _, _ := openService(t)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.ImportSeal(ctx, service.SealInput{Image: sealPNG(t, 240, 90)}); err == nil {
		t.Fatal("missing name should fail")
	}
	if _, err := svc.ImportSeal(ctx, service.SealInput{Name: "公章", PartyID: "pty_nope", Image: sealPNG(t, 240, 90)}); err == nil {
		t.Fatal("unknown party should fail")
	}
	if _, err := svc.ImportSeal(ctx, service.SealInput{Name: "公章", Image: []byte("nope")}); err == nil {
		t.Fatal("invalid image should fail")
	}
	seal, err := svc.ImportSeal(ctx, service.SealInput{Name: "公章", Image: sealPNG(t, 240, 90)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachSealToContracts(ctx, seal.ID, nil); err == nil {
		t.Fatal("empty contract list should fail")
	}
	if _, err := svc.AttachSealToContracts(ctx, seal.ID, []string{"ctr_nope"}); err == nil {
		t.Fatal("unknown contract should fail")
	}
	if _, err := svc.AttachSealToContracts(ctx, "seal_nope", []string{"ctr_nope"}); err == nil {
		t.Fatal("unknown seal should fail")
	}
}
