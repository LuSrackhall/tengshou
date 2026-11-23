// 白盒测试：.docx 导入的服务层编排（评审 FIX-1，te-1mgk.14）。
//
// 现有 service 测试全部走 PDF 直导，.docx → 转换器 → template.pdf 这段编排
// 没有经过测试。本文件通过替换未导出的 converters 链注入假转换器，在无
// Word / LibreOffice 的机器上也能覆盖成功链与「无可用转换器」失败链。
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/doctopdf"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// fakeDocxConverter stands in for Word/LibreOffice. Convert writes a blank
// single-page PDF; the fake never touches the .docx content.
type fakeDocxConverter struct {
	name      string
	available bool
}

func (f *fakeDocxConverter) Name() string  { return f.name }
func (f *fakeDocxConverter) Label() string { return "测试转换器" }
func (f *fakeDocxConverter) Available() (bool, string) {
	return f.available, "测试注入"
}
func (f *fakeDocxConverter) Hint() string { return "测试环境无需安装转换程序" }

func (f *fakeDocxConverter) Convert(_ context.Context, docxPath, pdfPath string) error {
	if _, err := os.Stat(docxPath); err != nil {
		return err
	}
	return writeBlankPDF(pdfPath)
}

func writeBlankPDF(dest string) error {
	var buf bytes.Buffer
	const wMM, hMM = 210.0, 297.0
	r := pdf.New(&buf, wMM, hMM, &pdf.Options{Compress: true})
	c := canvas.New(wMM, hMM)
	c.RenderTo(r)
	if err := r.Close(); err != nil {
		return err
	}
	return os.WriteFile(dest, buf.Bytes(), 0o600)
}

func newInternalTemplateService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rd, err := st.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	svc := New(st, rd, "测试员")
	svc.SetDataDir(t.TempDir())
	return svc
}

// minimalDocxBytes builds the smallest valid OOXML package so the import
// sniffs it as a Word document (ZIP magic + .docx extension).
func minimalDocxBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
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
	return buf.Bytes()
}

func TestImportTemplateDocxUsesConverter(t *testing.T) {
	svc := newInternalTemplateService(t)
	svc.converters = &doctopdf.Chain{Converters: []doctopdf.Converter{
		&fakeDocxConverter{name: "test-word", available: true},
	}}

	res, err := svc.ImportTemplate(context.Background(), TemplateImportInput{
		Name: "Word 导入模板", FileName: "房屋租赁合同.docx", Data: minimalDocxBytes(t),
	})
	if err != nil {
		t.Fatalf("import docx: %v", err)
	}
	tpl := res.Template
	if tpl.Kind != model.TemplateKindPDF || tpl.Version != 1 || tpl.PageCount != 1 {
		t.Fatalf("template = kind %q v%d pages %d", tpl.Kind, tpl.Version, tpl.PageCount)
	}
	if tpl.Converter != "test-word" {
		t.Fatalf("converter = %q, want test-word", tpl.Converter)
	}
	wantSource := path.Join("templates", "pdf", tpl.ID, "source.docx")
	if tpl.SourcePath != wantSource {
		t.Fatalf("source path = %q, want %q", tpl.SourcePath, wantSource)
	}
	wantPDF := path.Join("templates", "pdf", tpl.ID, "template.pdf")
	if tpl.PDFPath != wantPDF {
		t.Fatalf("pdf path = %q, want %q", tpl.PDFPath, wantPDF)
	}

	sourceRaw, err := os.ReadFile(filepath.Join(svc.dataDir, filepath.FromSlash(tpl.SourcePath)))
	if err != nil {
		t.Fatalf("read stored source: %v", err)
	}
	if !bytes.HasPrefix(sourceRaw, []byte("PK\x03\x04")) {
		t.Fatalf("stored source is not the original docx: %q", sourceRaw[:4])
	}
	pdfRaw, err := os.ReadFile(filepath.Join(svc.dataDir, filepath.FromSlash(tpl.PDFPath)))
	if err != nil {
		t.Fatalf("read converted pdf: %v", err)
	}
	if !bytes.HasPrefix(pdfRaw, []byte("%PDF")) {
		t.Fatalf("converted file is not a PDF: %q", pdfRaw[:4])
	}
}

func TestImportTemplateDocxWithoutConverter(t *testing.T) {
	svc := newInternalTemplateService(t)
	svc.converters = &doctopdf.Chain{Converters: []doctopdf.Converter{
		&fakeDocxConverter{name: "test-word", available: false},
	}}

	_, err := svc.ImportTemplate(context.Background(), TemplateImportInput{
		Name: "无转换器模板", FileName: "房屋租赁合同.docx", Data: minimalDocxBytes(t),
	})
	if err == nil {
		t.Fatal("expected import to fail without an available converter")
	}
	var be *Error
	if !errors.As(err, &be) || be.Code != "converter_unavailable" {
		t.Fatalf("error = %v, want business code converter_unavailable", err)
	}

	// The failed import must not leave an asset directory or a template record.
	entries, readErr := os.ReadDir(filepath.Join(svc.dataDir, "templates", "pdf"))
	if readErr == nil && len(entries) != 0 {
		t.Fatalf("failed import left %d asset dirs", len(entries))
	}
	list, err := svc.ListTemplates(context.Background(), model.ListFilter{})
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("failed import persisted %d templates", list.Total)
	}
}
