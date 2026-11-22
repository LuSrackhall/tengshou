package demo_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/app"
	"github.com/LuSrackhall/tengshou/internal/demo"
)

func TestDemoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	a, err := app.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	gen, err := a.Generator()
	if err != nil {
		t.Skipf("no CJK font available: %v", err)
	}
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	res, err := demo.Run(ctx, a.Store, gen, a.Paths, now)
	if err != nil {
		t.Fatalf("demo: %v", err)
	}
	if _, err := os.Stat(res.PDFPath); err != nil {
		t.Fatalf("pdf missing: %v", err)
	}
	if res.Document.PageCount < 2 {
		t.Fatalf("page count = %d", res.Document.PageCount)
	}
	if res.Signature.SHA256 == "" || res.Signature.WidthPx == 0 {
		t.Fatalf("signature = %+v", res.Signature)
	}
	if _, err := os.Stat(filepath.Join(a.Paths.Root, res.Signature.AssetPath)); err != nil {
		t.Fatalf("signature file missing: %v", err)
	}
	for _, rel := range []string{"tengshou.db", "config.json", "templates/contract_basic.html"} {
		if _, err := os.Stat(filepath.Join(a.Paths.Root, rel)); err != nil {
			t.Fatalf("%s missing: %v", rel, err)
		}
	}
	stats, err := a.Reader.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Parties != 2 || stats.Contracts != 1 || stats.Templates != 1 || stats.Signatures != 1 || stats.Documents != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	detail, err := a.Reader.GetContractDetail(ctx, res.Contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.PartyA == nil || detail.PartyB == nil {
		t.Fatalf("detail parties missing: %+v", detail)
	}

	// Re-running is idempotent for seed records (documents accumulate by id).
	res2, err := demo.Run(ctx, a.Store, gen, a.Paths, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second demo: %v", err)
	}
	stats2, _ := a.Reader.Stats(ctx)
	if stats2.Parties != 2 || stats2.Templates != 1 || stats2.Signatures != 1 || stats2.Documents != 1 {
		t.Fatalf("stats after rerun = %+v", stats2)
	}
	if res2.PDFPath == "" {
		t.Fatal("second run produced no pdf")
	}
}

func TestBackupFromApp(t *testing.T) {
	dir := t.TempDir()
	a, err := app.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	path, err := a.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}
