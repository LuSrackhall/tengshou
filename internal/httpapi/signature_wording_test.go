package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSignatureWordingBoundary keeps the LAN signature copy inside the
// document-tool boundary: it captures a handwritten signature, never a signing
// platform promise. The policy document docs/b7-renewal.md is excluded because
// it explains the D3 boundary by name; every other user-facing file must stay
// clean, including the Vue signature view migrated out of app.js.
func TestSignatureWordingBoundary(t *testing.T) {
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
	// 阶段 1：扫码签名对话框、签名摘要与导出已迁到 Vue，界面文案落在这里，
	// 边界检查随迁移覆盖。
	for _, pattern := range []string{
		"../../frontend/src/components/signatures/*.vue",
		"../../frontend/src/components/signatures/*.ts",
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
				t.Errorf("%s contains forbidden signature wording %q", file, phrase)
			}
		}
	}
}
