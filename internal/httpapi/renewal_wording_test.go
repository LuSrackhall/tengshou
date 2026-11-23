package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenewalWordingBoundary keeps B7 copy inside the document-tool boundary:
// renewal produces an editable draft, never a signing-platform promise. The
// policy document docs/b7-renewal.md is excluded because it explains the D3
// boundary by name; every other user-facing file must stay clean.
func TestRenewalWordingBoundary(t *testing.T) {
	forbidden := []string{"直接签订", "在线签约", "代签"}
	files := []string{
		"web/app/index.html",
		"web/app/app.css",
		"web/sign.html",
		"../../README.md",
	}
	// app.js 退役后界面文案全部在 frontend/src（Vue/SFC + TS）里。
	files = append(files, frontendSourceFiles(t)...)
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if strings.HasSuffix(filepath.ToSlash(doc), "docs/b7-renewal.md") {
			continue
		}
		files = append(files, doc)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(raw)
		for _, phrase := range forbidden {
			if strings.Contains(text, phrase) {
				t.Errorf("%s contains forbidden renewal wording %q", file, phrase)
			}
		}
	}
}
