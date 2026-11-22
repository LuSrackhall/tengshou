package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ledongthucpdf "github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
)

const (
	testPageW = 595.28 // A4 width in points
	testPageH = 841.89 // A4 height in points
)

func testFont(t *testing.T) *canvas.FontFamily {
	t.Helper()
	family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{})
	if err != nil {
		t.Skipf("no CJK font available: %v", err)
	}
	return family
}

// writeTemplatePDF writes a small multi-page PDF that stands in for a Word
// document converted to PDF.
func writeTemplatePDF(t *testing.T, path string, pages int, family *canvas.FontFamily) {
	t.Helper()
	wMM := testPageW * mmPerPt
	hMM := testPageH * mmPerPt
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	r := pdf.New(f, wMM, hMM, &pdf.Options{Compress: true, SubsetFonts: true})
	for i := 0; i < pages; i++ {
		if i > 0 {
			r.NewPage(wMM, hMM)
		}
		c := canvas.New(wMM, hMM)
		ctx := canvas.NewContext(c)
		face := family.Face(16, color.RGBA{R: 0, G: 0, B: 0, A: 255}, canvas.FontRegular)
		ctx.DrawText(40, hMM-40, canvas.NewTextLine(face, fmt.Sprintf("模板第%d页", i+1), canvas.Left))
		ctx.DrawText(40, hMM-70, canvas.NewTextLine(face, "甲方（盖章）：", canvas.Left))
		c.RenderTo(r)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if x%7 == 0 || y%5 == 0 {
				img.Set(x, y, color.RGBA{R: 200, G: 20, B: 20, A: 255})
			}
		}
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
}

type testResolver struct {
	text   map[string]string
	images map[string]image.Image
}

func (r testResolver) Text(p model.Placement) (string, bool) {
	v, ok := r.text[p.Field]
	return v, ok
}

func (r testResolver) Image(p model.Placement) (image.Image, bool, error) {
	img, ok := r.images[p.ID]
	return img, ok, nil
}

func TestBuildAndStampOverlay(t *testing.T) {
	family := testFont(t)
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pdf")
	writeTemplatePDF(t, templatePath, 2, family)
	pngPath := filepath.Join(dir, "sig.png")
	writeTestPNG(t, pngPath, 60, 24)
	pngFile, err := os.Open(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := png.Decode(pngFile)
	pngFile.Close()
	if err != nil {
		t.Fatal(err)
	}

	pages, err := PageSizes(templatePath)
	if err != nil {
		t.Fatalf("page sizes: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	if diff := pages[0].Width - testPageW; diff > 1 || diff < -1 {
		t.Fatalf("page width = %.2f, want ~%.2f", pages[0].Width, testPageW)
	}

	placements := []model.Placement{
		{ID: "p1", Page: 1, X: 120, Y: 120, W: 220, H: 20, Type: model.PlacementField, Field: "contract.contract_no"},
		{ID: "p2", Page: 1, X: 120, Y: 160, W: 260, H: 60, Type: model.PlacementField, Field: "contract.data.address"},
		{ID: "p3", Page: 2, X: 120, Y: 500, W: 140, H: 60, Type: model.PlacementSignature, Party: model.PlacementPartyB},
		{ID: "p4", Page: 2, X: 380, Y: 480, W: 120, H: 120, Type: model.PlacementSeal, Party: model.PlacementPartyA},
	}
	resolver := testResolver{
		text: map[string]string{
			"contract.contract_no":  "HT-2026-0001",
			"contract.data.address": "幸福村 3 组 12 号房屋租赁合同",
		},
		images: map[string]image.Image{"p3": sig, "p4": sig},
	}
	res, err := Build(Options{
		Pages:      pages,
		Placements: placements,
		Font:       family,
		Resolver:   resolver,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", res.Warnings)
	}
	outPath := filepath.Join(dir, "out.pdf")
	if err := Stamp(templatePath, res.PDF, outPath); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	outPages, err := PageSizes(outPath)
	if err != nil {
		t.Fatalf("output page sizes: %v", err)
	}
	if len(outPages) != 2 {
		t.Fatalf("output pages = %d, want 2", len(outPages))
	}
	if diff := outPages[1].Width - testPageW; diff > 1 || diff < -1 {
		t.Fatalf("output page width = %.2f", outPages[1].Width)
	}

	// The overlay text lives in its own PDF page content, so it is readable
	// before stamping. After stamping it sits inside a Form XObject, which the
	// extraction library used here does not descend into; assert the template
	// text is still intact and that the stamped resources are present.
	overlayPath := filepath.Join(dir, "overlay.pdf")
	if err := os.WriteFile(overlayPath, res.PDF, 0o600); err != nil {
		t.Fatal(err)
	}
	overlayText := extractText(t, overlayPath)
	for _, want := range []string{"HT-2026-0001", "幸福村"} {
		if !strings.Contains(overlayText, want) {
			t.Fatalf("overlay PDF text does not contain %q\n%s", want, overlayText)
		}
	}

	text := extractText(t, outPath)
	for _, want := range []string{"模板第1页", "模板第2页", "甲方（盖章）"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output PDF text does not contain %q\n%s", want, text)
		}
	}
	assertStamped(t, outPath, 2)
}

// assertStamped verifies that every page has an added XObject resource and the
// catalog has the watermark optional-content group pdfcpu creates.
func assertStamped(t *testing.T, path string, pages int) {
	t.Helper()
	ctx, err := api.ReadContextFile(path)
	if err != nil {
		t.Fatalf("read stamped pdf: %v", err)
	}
	if err := ctx.EnsurePageCount(); err != nil {
		t.Fatal(err)
	}
	if ctx.PageCount != pages {
		t.Fatalf("page count = %d, want %d", ctx.PageCount, pages)
	}
	for i := 1; i <= pages; i++ {
		d, _, _, err := ctx.PageDict(i, false)
		if err != nil {
			t.Fatalf("page dict %d: %v", i, err)
		}
		obj, found := d.Find("Resources")
		if !found {
			t.Fatalf("page %d has no resources", i)
		}
		resDict, err := ctx.DereferenceDict(obj)
		if err != nil {
			t.Fatal(err)
		}
		xobj, found := resDict.Find("XObject")
		if !found {
			t.Fatalf("page %d has no XObject resources", i)
		}
		xDict, err := ctx.DereferenceDict(xobj)
		if err != nil {
			t.Fatal(err)
		}
		if len(xDict) == 0 {
			t.Fatalf("page %d XObject resources are empty", i)
		}
	}
	root, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := root.Find("OCProperties"); !found {
		t.Fatal("stamped pdf has no optional-content properties")
	}
}

func TestBuildWarnsOnOverflowAndMissingImage(t *testing.T) {
	family := testFont(t)
	pages := []PageSize{{Width: testPageW, Height: testPageH}}
	res, err := Build(Options{
		Pages: pages,
		Placements: []model.Placement{
			{ID: "long", Page: 1, X: 40, Y: 40, W: 30, H: 12, Type: model.PlacementField, Field: "contract.data.clause"},
			{ID: "sig", Page: 1, X: 40, Y: 100, W: 100, H: 40, Type: model.PlacementSignature},
		},
		Font: family,
		Resolver: testResolver{
			text: map[string]string{
				"contract.data.clause": "甲方将位于幸福村三组十二号的房屋出租给乙方居住使用租赁期限自二零二六年十二月一日起至二零二七年十一月三十日止",
			},
		},
		Placeholder: true,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	codes := map[string]bool{}
	for _, w := range res.Warnings {
		codes[w.Code] = true
	}
	if !codes["text_overflow"] {
		t.Fatalf("missing text_overflow warning: %#v", res.Warnings)
	}
	if !codes["missing_image"] {
		t.Fatalf("missing missing_image warning: %#v", res.Warnings)
	}
	if len(res.PDF) == 0 {
		t.Fatal("overlay PDF is empty")
	}
}

func TestBuildRejectsPageOutOfRange(t *testing.T) {
	family := testFont(t)
	_, err := Build(Options{
		Pages: []PageSize{{Width: 400, Height: 500}},
		Placements: []model.Placement{
			{ID: "p", Page: 2, X: 10, Y: 10, W: 50, H: 10, Type: model.PlacementField, Field: "contract.title"},
		},
		Font:     family,
		Resolver: testResolver{},
	})
	if err == nil {
		t.Fatal("expected out-of-range page error")
	}
	if !strings.Contains(err.Error(), "页码") {
		t.Fatalf("error = %v", err)
	}
}

func TestWrapTextBreaksChineseAndLatin(t *testing.T) {
	family := testFont(t)
	face := family.Face(10.5, color.RGBA{A: 255}, canvas.FontRegular)
	lines := wrapText(face, "甲方将位于幸福村三组十二号的房屋出租给乙方", 30)
	if len(lines) < 2 {
		t.Fatalf("expected Chinese text to wrap, got %#v", lines)
	}
	lines = wrapText(face, "This is a long English sentence that must wrap across lines", 30)
	if len(lines) < 2 {
		t.Fatalf("expected Latin text to wrap, got %#v", lines)
	}
	if got := strings.Join(lines, ""); !strings.Contains(got, "English") {
		t.Fatalf("wrapped text lost content: %#v", lines)
	}
}

func extractText(t *testing.T, path string) string {
	t.Helper()
	f, r, err := ledongthucpdf.Open(path)
	if err != nil {
		t.Fatalf("open generated pdf: %v", err)
	}
	defer f.Close()
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			t.Fatalf("extract text page %d: %v", i, err)
		}
		b.WriteString(text)
	}
	return b.String()
}

func TestPageSizesAndStampHandleRotation(t *testing.T) {
	family := testFont(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.pdf")
	writeTemplatePDF(t, base, 1, family)
	rotated := filepath.Join(dir, "rotated.pdf")
	if err := api.RotateFile(base, rotated, 90, nil, nil); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	pages, err := PageSizes(rotated)
	if err != nil {
		t.Fatalf("page sizes: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d", len(pages))
	}
	if math.Abs(pages[0].Width-testPageH) > 1 || math.Abs(pages[0].Height-testPageW) > 1 {
		t.Fatalf("rotated size = %.1f×%.1f, want %.1f×%.1f",
			pages[0].Width, pages[0].Height, testPageH, testPageW)
	}
	res, err := Build(Options{
		Pages: pages,
		Placements: []model.Placement{
			{ID: "p", Page: 1, X: 60, Y: 60, W: 220, H: 24, Type: model.PlacementField, Field: "contract.title"},
		},
		Font:     family,
		Resolver: testResolver{text: map[string]string{"contract.title": "旋转页套印测试"}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out := filepath.Join(dir, "rotated-out.pdf")
	if err := Stamp(rotated, res.PDF, out); err != nil {
		t.Fatalf("stamp rotated: %v", err)
	}
	outPages, err := PageSizes(out)
	if err != nil {
		t.Fatalf("output page sizes: %v", err)
	}
	if len(outPages) != 1 ||
		math.Abs(outPages[0].Width-pages[0].Width) > 1 ||
		math.Abs(outPages[0].Height-pages[0].Height) > 1 {
		t.Fatalf("output size = %#v, want %#v", outPages, pages)
	}
	assertStamped(t, out, 1)
}
