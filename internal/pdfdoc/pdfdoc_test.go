package pdfdoc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	ledongthucpdf "github.com/ledongthuc/pdf"
)

func testGenerator(t *testing.T) *Generator {
	t.Helper()
	gen, err := NewGenerator(FontOptions{}, 20, true)
	if err != nil {
		t.Skipf("no CJK font available: %v", err)
	}
	return gen
}

func writeTestPNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if x%17 < 3 || y%23 < 2 {
				img.Set(x, y, color.RGBA{10, 10, 10, 255})
			}
		}
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const testTemplate = `<!doctype html>
<html><body>
<h1 style="text-align:center;font-size:20pt">房屋租赁合同</h1>
<p>甲方（出租方）：{{.PartyA.Name}}</p>
<p>乙方（承租方）：{{.PartyB.Name}}</p>
<p style="text-indent:8mm">根据《中华人民共和国民法典》有关规定，甲乙双方在平等、自愿的基础上，就房屋租赁事宜达成如下协议：</p>
<table style="font-size:10pt">
<tr><th>项目</th><th>内容</th></tr>
<tr><td>房屋地址</td><td>{{.Data.address}}</td></tr>
<tr><td>月租金</td><td>{{money .Data.rent}} 元</td></tr>
</table>
<p>乙方签名：</p>
<p><img src="assets/signature.png" style="width:50mm"></p>
<div class="page-break"></div>
<h2>第二条 双方权利义务</h2>
<p>{{.Data.long_clause}}</p>
<p>签署日期：{{cnDate .Data.signed_on}}</p>
</body></html>`

func TestRenderChineseContractPDF(t *testing.T) {
	gen := testGenerator(t)
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "assets"), "signature.png", 240, 100)

	long := strings.Repeat("甲乙双方应当依照约定履行各自的义务，任何一方不得擅自变更或者解除合同。", 12)
	data := TemplateData{
		Data: map[string]any{
			"address":     "幸福村 12 号",
			"rent":        1200,
			"signed_on":   "2026-11-22",
			"long_clause": long,
		},
	}
	partyA := model.Party{Name: "幸福村股份经济合作社", Kind: model.PartyOrganization}
	partyB := model.Party{Name: "张三", Kind: model.PartyPerson}
	data.PartyA = &partyA
	data.PartyB = &partyB

	doc, err := gen.RenderHTML(testTemplate, data, dir, "房屋租赁合同")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if doc.PageCount < 2 {
		t.Fatalf("expected at least 2 pages, got %d", doc.PageCount)
	}
	if len(doc.Missing) != 0 {
		t.Fatalf("missing glyphs: %q", string(doc.Missing))
	}
	out := filepath.Join(dir, "contract.pdf")
	sha, size, err := WritePDFFile(doc, out, "房屋租赁合同")
	if err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if len(sha) != 64 || size <= 0 {
		t.Fatalf("bad artifact metadata sha=%q size=%d", sha, size)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("/Subtype /Image")) && !bytes.Contains(raw, []byte("/Image")) {
		t.Fatalf("pdf does not contain an embedded image")
	}
	f, r, err := ledongthucpdf.Open(out)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer f.Close()
	if r.NumPage() != doc.PageCount {
		t.Fatalf("pdf page count %d != %d", r.NumPage(), doc.PageCount)
	}
	page1 := r.Page(1)
	text, err := page1.GetPlainText(nil)
	if err != nil {
		t.Fatalf("extract text: %v", err)
	}
	if !strings.Contains(text, "甲方") || !strings.Contains(text, "房屋租赁合同") {
		t.Fatalf("generated PDF text missing Chinese content: %q", text)
	}
}

func TestRenderHardPageBreak(t *testing.T) {
	gen := testGenerator(t)
	dir := t.TempDir()
	doc, err := gen.RenderHTML(`<p>第一页内容</p><div class="page-break"></div><p>第二页内容</p>`, TemplateData{}, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount != 2 {
		t.Fatalf("expected 2 pages, got %d", doc.PageCount)
	}
}

func TestRenderTableAcrossPages(t *testing.T) {
	gen := testGenerator(t)
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("<table><tr><th>序号</th><th>条款</th></tr>")
	for i := 0; i < 120; i++ {
		b.WriteString("<tr><td>")
		b.WriteString(strings.Repeat("项", 1))
		b.WriteString("</td><td>")
		b.WriteString(strings.Repeat("合同条款内容", 4))
		b.WriteString("</td></tr>")
	}
	b.WriteString("</table>")
	doc, err := gen.RenderHTML(b.String(), TemplateData{}, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount < 2 {
		t.Fatalf("expected table to span pages, got %d", doc.PageCount)
	}
}

func TestExecuteTemplateStrictMissingKey(t *testing.T) {
	_, err := ExecuteTemplate(`{{.Data.not_there}}`, TemplateData{Data: map[string]any{}})
	if err == nil {
		t.Fatal("expected missingkey error")
	}
}

func TestExtractVariables(t *testing.T) {
	got := ExtractVariables(`{{.Data.rent}} {{ .Data.address }} {{.Contract.Title}}`)
	want := []string{"address", "rent"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestRejectUnsafeImagePath(t *testing.T) {
	gen := testGenerator(t)
	dir := t.TempDir()
	_, err := gen.RenderHTML(`<p><img src="../../etc/passwd"></p>`, TemplateData{}, dir, "")
	if err == nil {
		t.Fatal("expected unsafe image path to fail")
	}
}
