package service_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/overlay"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

type templateFixture struct {
	svc     *service.Service
	rd      *store.Reader
	st      *store.Store
	dataDir string
	font    *canvas.FontFamily
}

func newTemplateFixture(t *testing.T) *templateFixture {
	t.Helper()
	svc, rd, st := openService(t)
	family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{})
	if err != nil {
		t.Skipf("no CJK font available: %v", err)
	}
	dataDir := t.TempDir()
	svc.SetDataDir(dataDir)
	svc.SetFontLoader(func() (*canvas.FontFamily, error) { return family, nil })
	return &templateFixture{svc: svc, rd: rd, st: st, dataDir: dataDir, font: family}
}

func (f *templateFixture) templatePDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	const wMM, hMM = 210.0, 297.0
	r := pdf.New(&buf, wMM, hMM, &pdf.Options{Compress: true, SubsetFonts: true})
	c := canvas.New(wMM, hMM)
	ctx := canvas.NewContext(c)
	face := f.font.Face(16, color.RGBA{A: 255}, canvas.FontRegular)
	ctx.DrawText(25, 270, canvas.NewTextLine(face, "房屋租赁合同（模板）", canvas.Left))
	ctx.DrawText(25, 250, canvas.NewTextLine(face, "甲方（出租方）：", canvas.Left))
	ctx.DrawText(25, 235, canvas.NewTextLine(face, "乙方（承租方）：", canvas.Left))
	c.RenderTo(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (f *templateFixture) writePNG(t *testing.T, rel string) (string, int, int) {
	t.Helper()
	abs := filepath.Join(f.dataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}
	const w, h = 80, 30
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if x%9 < 2 {
				img.Set(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
			}
		}
	}
	out, err := os.Create(abs)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return rel, w, h
}

func (f *templateFixture) importTemplate(t *testing.T, name string) service.TemplateImportResult {
	t.Helper()
	res, err := f.svc.ImportTemplate(context.Background(), service.TemplateImportInput{
		Name: name, FileName: name + ".pdf", Data: f.templatePDF(t),
	})
	if err != nil {
		t.Fatalf("import template: %v", err)
	}
	return res
}

func TestTemplateImportCalibratePreviewGenerate(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()

	// A dynamic money field and a long text field exercise formatting and
	// overflow reporting.
	if _, err := f.svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "rent", Label: "月租金",
		Type: model.FieldTypeMoney,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "clause", Label: "条款正文",
		Type: model.FieldTypeLongText,
	}); err != nil {
		t.Fatal(err)
	}
	party := mustParty(t, f.svc, "张三")
	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "幸福村房屋租赁合同", PartyBID: party.ID, Amount: 1200,
		SignedOn: "2026-01-01",
		Data: map[string]any{
			"rent":   1200,
			"clause": "甲方将位于幸福村三组十二号的房屋出租给乙方居住使用，租赁期限自二零二六年十二月一日起至二零二七年十一月三十日止。",
		},
	})
	sigRel, _, _ := f.writePNG(t, "assets/signatures/sig1.png")
	sig := model.Signature{PartyID: party.ID, SignerName: party.Name, AssetPath: sigRel,
		SHA256: "sigsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}
	sealRel, _, _ := f.writePNG(t, "assets/seals/seal1.png")
	seal := model.Seal{Name: "示例公章", AssetPath: sealRel, SHA256: "sealsha", WidthPx: 80, HeightPx: 30}
	if err := f.st.PutSeal(ctx, &seal); err != nil {
		t.Fatal(err)
	}

	imported := f.importTemplate(t, "房屋租赁模板")
	if imported.Template.Kind != model.TemplateKindPDF || imported.Template.Version != 1 {
		t.Fatalf("imported = %+v", imported.Template)
	}
	if imported.Template.Converter != "pdf" || imported.Template.PageCount != 1 {
		t.Fatalf("converter/page count = %q/%d", imported.Template.Converter, imported.Template.PageCount)
	}

	saved, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "no", Page: 1, X: 120, Y: 150, W: 200, H: 22, Type: model.PlacementField, Field: "contract.contract_no"},
			{ID: "rent", Page: 1, X: 120, Y: 200, W: 160, H: 22, Type: model.PlacementField, Field: "contract.data.rent"},
			{ID: "clause", Page: 1, X: 60, Y: 260, W: 60, H: 20, Type: model.PlacementField, Field: "contract.data.clause"},
			{ID: "sig", Page: 1, X: 300, Y: 600, W: 140, H: 60, Type: model.PlacementSignature, Party: model.PlacementPartyB},
			{ID: "seal", Page: 1, X: 120, Y: 600, W: 120, H: 120, Type: model.PlacementSeal,
				SealMode: model.PlacementSealFixed, SealID: seal.ID},
		},
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	if saved.Template.Version != 2 || len(saved.Template.Placements) != 5 {
		t.Fatalf("saved = v%d, %d placements", saved.Template.Version, len(saved.Template.Placements))
	}
	tpl := saved.Template

	// Preview with sample data reports the intentionally tiny clause box.
	preview, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{Sample: true})
	if err != nil {
		t.Fatalf("sample preview: %v", err)
	}
	if len(preview.PDF) == 0 || preview.PageCount != 1 || len(preview.PreviewSHA256) != 64 {
		t.Fatalf("preview = pages %d sha %q bytes %d", preview.PageCount, preview.PreviewSHA256, len(preview.PDF))
	}

	// Real preview uses the contract data and its signature/seal pictures.
	realPreview, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("contract preview: %v", err)
	}
	if hasWarning(realPreview.Warnings, "missing_image") {
		t.Fatalf("unexpected missing image warning: %#v", realPreview.Warnings)
	}
	if !hasWarning(realPreview.Warnings, "text_overflow") {
		t.Fatalf("expected text_overflow warning: %#v", realPreview.Warnings)
	}
	if realPreview.ContractID != contract.ID {
		t.Fatalf("preview contract = %q", realPreview.ContractID)
	}

	// Generation is refused without the preview digest.
	if _, err := f.svc.GenerateContractPDF(ctx, tpl.ID, contract.ID, ""); err == nil ||
		!strings.Contains(err.Error(), "预览") {
		t.Fatalf("generate without preview = %v", err)
	}
	gen, err := f.svc.GenerateContractPDF(ctx, tpl.ID, contract.ID, realPreview.PreviewSHA256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gen.Document.TemplateID != tpl.ID || gen.Document.TemplateVersion != 2 || gen.Document.PageCount != 1 {
		t.Fatalf("document = %+v", gen.Document)
	}
	outAbs := filepath.Join(f.dataDir, filepath.FromSlash(gen.Document.FilePath))
	raw, err := os.ReadFile(outAbs)
	if err != nil {
		t.Fatalf("output file: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatalf("output is not a PDF")
	}
	if gen.Usage.VersionContracts != 1 || gen.Usage.Documents != 1 {
		t.Fatalf("usage = %+v", gen.Usage)
	}
	// Generating pins the contract to this template version.
	stored, err := f.rd.GetContract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.TemplateID != tpl.ID {
		t.Fatalf("contract template = %q, want %q", stored.TemplateID, tpl.ID)
	}

	// A later version does not disturb the pinned contract or existing PDF.
	later, err := f.svc.SaveTemplateMapping(ctx, tpl.ID, service.TemplateSaveInput{
		Placements: tpl.Placements,
	})
	if err != nil {
		t.Fatalf("save later version: %v", err)
	}
	if later.Template.Version != 3 {
		t.Fatalf("later version = %d", later.Template.Version)
	}
	if later.Usage.OtherVersionContracts != 1 {
		t.Fatalf("usage after v3 = %+v", later.Usage)
	}
	previewOld, err := f.svc.PreviewTemplate(ctx, tpl.ID, service.TemplatePreviewInput{ContractID: contract.ID})
	if err != nil {
		t.Fatalf("preview pinned version: %v", err)
	}
	if previewOld.PreviewSHA256 == "" {
		t.Fatal("expected preview digest")
	}

	// Referenced versions cannot be deleted; the fresh v3 can.
	if err := f.svc.DeleteTemplateVersion(ctx, tpl.ID); err == nil {
		t.Fatal("expected referenced template deletion to fail")
	}
	if err := f.svc.DeleteTemplateVersion(ctx, later.Template.ID); err != nil {
		t.Fatalf("delete unreferenced version: %v", err)
	}
}

func TestTemplateMappingRejectsBadBindings(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	imported := f.importTemplate(t, "校验模板")
	if _, err := f.svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "photo", Label: "照片",
		Type: model.FieldTypeImage,
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		p    model.Placement
		want string
	}{
		{"unknown field", model.Placement{ID: "a", Page: 1, X: 10, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "contract.nope"}, "未知的合同字段"},
		{"unknown data field", model.Placement{ID: "b", Page: 1, X: 10, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "contract.data.nope"}, "不在合同字段中"},
		{"image field", model.Placement{ID: "c", Page: 1, X: 10, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "contract.data.photo"}, "不能作为文字绑定"},
		{"bad prefix", model.Placement{ID: "d", Page: 1, X: 10, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "unknown.thing"}, "开头"},
		{"out of page", model.Placement{ID: "e", Page: 1, X: 900, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "contract.title"}, "超出第 1 页范围"},
		{"bad page", model.Placement{ID: "f", Page: 3, X: 10, Y: 10, W: 100, H: 20,
			Type: model.PlacementField, Field: "contract.title"}, "页码"},
		{"signature without party", model.Placement{ID: "g", Page: 1, X: 10, Y: 10, W: 100, H: 40,
			Type: model.PlacementSignature}, "甲方或乙方"},
		{"fixed seal without id", model.Placement{ID: "h", Page: 1, X: 10, Y: 10, W: 100, H: 100,
			Type: model.PlacementSeal, SealMode: model.PlacementSealFixed}, "公章图片"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
				Placements: []model.Placement{tc.p},
			})
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want contain %q", err, tc.want)
			}
		})
	}
}

func TestTemplateVersionsAndReimport(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	first := f.importTemplate(t, "版本模板")
	if first.Template.Version != 1 {
		t.Fatal(first.Template.Version)
	}
	second := f.importTemplate(t, "版本模板")
	if second.Template.Version != 2 || second.Template.ID == first.Template.ID {
		t.Fatalf("second = v%d id=%s", second.Template.Version, second.Template.ID)
	}
	if second.Usage.FamilyVersions != 2 {
		t.Fatalf("usage = %+v", second.Usage)
	}
	versions, err := f.rd.ListTemplateVersions(ctx, "版本模板")
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %d err=%v", len(versions), err)
	}
	// The newest version is the active one.
	if !versions[0].IsActive || versions[1].IsActive {
		t.Fatalf("active flags = %v %v", versions[0].IsActive, versions[1].IsActive)
	}
	// Deleting an unreferenced version removes its asset directory.
	if err := f.svc.DeleteTemplateVersion(ctx, first.Template.ID); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	if _, err := f.rd.GetTemplate(ctx, first.Template.ID); err == nil {
		t.Fatal("deleted version still readable")
	}
}

func hasWarning(warnings []overlay.Warning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
