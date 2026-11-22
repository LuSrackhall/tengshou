package doctopdf_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/doctopdf"
	"github.com/LuSrackhall/tengshou/internal/overlay"
)

type fakeConverter struct {
	name      string
	available bool
	reason    string
	write     []byte
	err       error
	used      bool
}

func (f *fakeConverter) Name() string  { return f.name }
func (f *fakeConverter) Label() string { return "假转换器 " + f.name }
func (f *fakeConverter) Hint() string  { return "" }
func (f *fakeConverter) Available() (bool, string) {
	return f.available, f.reason
}
func (f *fakeConverter) Convert(ctx context.Context, docxPath, pdfPath string) error {
	f.used = true
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(pdfPath, f.write, 0o600)
}

func TestWordUnavailableOutsideWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上不适用")
	}
	ok, reason := doctopdf.NewWord().Available()
	if ok {
		t.Fatal("Word converter must not be available outside Windows")
	}
	if !strings.Contains(reason, "Windows") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestChainTriesConvertersInOrder(t *testing.T) {
	first := &fakeConverter{name: "first", available: false, reason: "测试不可用"}
	second := &fakeConverter{name: "second", available: true, write: []byte("%PDF-1.4 fake")}
	chain := &doctopdf.Chain{Converters: []doctopdf.Converter{first, second}}
	dir := t.TempDir()
	used, err := chain.Convert(context.Background(), filepath.Join(dir, "a.docx"), filepath.Join(dir, "a.pdf"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if used != "second" || !second.used || first.used {
		t.Fatalf("used = %q first=%v second=%v", used, first.used, second.used)
	}
}

func TestChainReportsAllAttempts(t *testing.T) {
	chain := &doctopdf.Chain{Converters: []doctopdf.Converter{
		&fakeConverter{name: "first", available: false, reason: "缺少程序"},
		&fakeConverter{name: "second", available: true, err: os.ErrPermission},
	}}
	dir := t.TempDir()
	_, err := chain.Convert(context.Background(), filepath.Join(dir, "a.docx"), filepath.Join(dir, "a.pdf"))
	if err == nil {
		t.Fatal("expected conversion error")
	}
	if !doctopdf.IsUnavailable(err) {
		t.Fatalf("error is not UnavailableError: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"缺少程序", "permission", "LibreOffice", "Microsoft Word"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestLibreOfficeConversion(t *testing.T) {
	conv := doctopdf.NewLibreOffice()
	if ok, reason := conv.Available(); !ok {
		t.Skipf("LibreOffice 不可用：%s", reason)
	}
	dir := t.TempDir()
	docx := filepath.Join(dir, "sample.docx")
	writeMinimalDocx(t, docx)
	pdf := filepath.Join(dir, "sample.pdf")
	if err := conv.Convert(context.Background(), docx, pdf); err != nil {
		t.Fatalf("convert: %v", err)
	}
	raw, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "%PDF") {
		t.Fatalf("output is not a PDF: %q", raw[:min(40, len(raw))])
	}
	pages, err := overlay.PageSizes(pdf)
	if err != nil {
		t.Fatalf("read page sizes: %v", err)
	}
	if len(pages) == 0 || pages[0].Width <= 0 {
		t.Fatalf("pages = %#v", pages)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// writeMinimalDocx builds a one-paragraph Word document with the smallest
// valid OOXML package. It exists so the LibreOffice path can be exercised on
// machines that have LibreOffice installed.
func writeMinimalDocx(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>房屋租赁合同</w:t></w:r></w:p><w:sectPr/></w:body>
</w:document>`,
	}
	for name, content := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
