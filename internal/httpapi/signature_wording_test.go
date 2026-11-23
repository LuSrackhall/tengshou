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
	// 扫码签名 Vue 组件与其他界面源文件一样已被 frontendSourceFiles 全量覆盖，
	// 不再单独 glob components/signatures。
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
