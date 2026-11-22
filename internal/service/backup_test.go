package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func signatureInput(partyID string, png []byte) service.SignatureInput {
	sum := sha256.Sum256(png)
	return service.SignatureInput{
		PartyID:    partyID,
		SignerName: "张三",
		ImagePNG:   png,
		SHA256:     hex.EncodeToString(sum[:]),
	}
}

func TestExportSignatureIsVerifiedAndAudited(t *testing.T) {
	svc, rd, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	png := []byte("fake-but-stable-png-bytes")
	sig, err := svc.SaveSignature(ctx, signatureInput(party.ID, png))
	if err != nil {
		t.Fatalf("save signature: %v", err)
	}
	raw, name, err := svc.ExportSignature(ctx, sig.ID)
	if err != nil {
		t.Fatalf("export signature: %v", err)
	}
	if string(raw) != string(png) || name != sig.ID+".png" {
		t.Fatalf("exported signature = %q name=%q", raw, name)
	}
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntitySignature, EntityID: sig.ID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range logs.Items {
		if entry.Action == model.ChangeActionExport && strings.Contains(entry.Summary, "导出签名") {
			found = true
		}
	}
	if !found {
		t.Fatalf("signature export not audited: %+v", logs.Items)
	}

	// A signature file that no longer matches its recorded digest is refused.
	full := filepath.Join(dir, filepath.FromSlash(sig.AssetPath))
	if err := os.WriteFile(full, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ExportSignature(ctx, sig.ID); err == nil {
		t.Fatal("tampered signature should not be exported")
	}
}

func TestIntegrityReportFindsMissingAndOrphanAssets(t *testing.T) {
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	party := mustParty(t, svc, "李四")
	png := []byte("integrity-png")
	sig, err := svc.SaveSignature(ctx, signatureInput(party.ID, png))
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if !report.DatabaseOK || len(report.MissingFiles) != 0 || len(report.OrphanFiles) != 0 {
		t.Fatalf("clean report = %+v", report)
	}
	orphan := filepath.Join(dir, "assets", "signatures", "orphan.png")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(sig.AssetPath))); err != nil {
		t.Fatal(err)
	}
	report, err = svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.MissingFiles) != 1 || report.MissingFiles[0].Path != sig.AssetPath {
		t.Fatalf("missing files = %+v", report.MissingFiles)
	}
	if len(report.OrphanFiles) != 1 || report.OrphanFiles[0] != "assets/signatures/orphan.png" {
		t.Fatalf("orphan files = %+v", report.OrphanFiles)
	}
}
