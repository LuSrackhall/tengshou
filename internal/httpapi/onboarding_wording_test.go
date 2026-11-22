package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnboardingWordingBoundary keeps the B8 guide inside the document-tool
// boundary: it generates contracts and keeps signatures, never a signing
// platform promise. The B7 policy document is excluded because it explains its
// own boundary by name.
func TestOnboardingWordingBoundary(t *testing.T) {
	forbidden := []string{
		"在线签约", "直接签订", "电子签约", "电子签章平台", "具有法律效力的电子签名",
	}
	files := []string{
		"web/app/index.html",
		"web/app/app.js",
		"web/app/app.css",
		"web/sign.html",
		"../../README.md",
	}
	// 阶段 1：模板页与标定编辑器已迁到 Vue，界面文案落在这里，边界检查随迁移覆盖。
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
				t.Errorf("%s contains forbidden onboarding wording %q", file, phrase)
			}
		}
	}
}

// TestOnboardingCopyPresent pins the few copy anchors the B8 acceptance
// criteria depend on, so a later refactor cannot silently drop the guide's
// starting point or the sample-data boundary.
func TestOnboardingCopyPresent(t *testing.T) {
	checks := []struct {
		file string
		text string
	}{
		{"web/app/index.html", "我有一份上级模板"},
		{"web/app/index.html", "样例数据，不写入台账"},
		{"web/app/index.html", "本机自检"},
		{"../../frontend/src/components/templates/useTemplatePreviewGenerate.ts", "样例数据未写入台账"},
		{"../../docs/b8-onboarding.md", "试用数据与真实数据的边界"},
		{"../../docs/b8-onboarding.md", "不预置地方模板"},
		{"../../README.md", "第一次使用"},
	}
	for _, check := range checks {
		raw, err := os.ReadFile(check.file)
		if err != nil {
			t.Fatalf("read %s: %v", check.file, err)
		}
		if !strings.Contains(string(raw), check.text) {
			t.Errorf("%s must contain %q", check.file, check.text)
		}
	}
}
