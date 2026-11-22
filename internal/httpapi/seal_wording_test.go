package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSealWordingBoundary keeps user-facing seal copy inside the B13 boundary:
// the feature only places a picture into a PDF. The check covers the app UI and
// the project documents so later wording changes cannot drift.
func TestSealWordingBoundary(t *testing.T) {
	forbidden := []string{"电子签章", "法定认证", "电子认证", "电子公章"}
	files := []string{
		"web/app/index.html",
		"web/app/app.js",
		"web/app/app.css",
		"web/sign.html",
	}
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, docs...)
	// 阶段 1：公章页已迁到 Vue，界面文案落在这里，边界检查随迁移覆盖。
	vue, err := filepath.Glob("../../frontend/src/components/seals/*.vue")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, vue...)
	sealModules, err := filepath.Glob("../../frontend/src/components/seals/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, sealModules...)
	// 模板标定编辑器里的公章下拉/名称回显也已迁到 Vue；按扩展名分别取
	// 文件，避免 templates/ 下出现子目录时把目录当文件读取。
	templates, err := filepath.Glob("../../frontend/src/components/templates/*.vue")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, templates...)
	templateModules, err := filepath.Glob("../../frontend/src/components/templates/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, templateModules...)
	files = append(files, "../../README.md")
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
				t.Errorf("%s contains forbidden seal wording %q", file, phrase)
			}
		}
	}
}
