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
		"web/app/app.js",
		"web/app/app.css",
		"web/sign.html",
		"../../README.md",
	}
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
	// 阶段 1：合同台账与续签表单已迁到 Vue，界面文案落在这里，边界检查
	// 随迁移覆盖。
	for _, pattern := range []string{
		"../../frontend/src/components/contracts/*.vue",
		"../../frontend/src/components/contracts/*.ts",
	} {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matched...)
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
