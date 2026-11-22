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
