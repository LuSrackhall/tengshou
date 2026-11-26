package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// te-rccn0：标定保存时校验公章/签名框与字段框重叠，只提示不阻断；同时
// 验证「字段值白底遮盖」开关随版本保存并能跨版本沿用。
func TestTemplateSaveOverlapWarningsAndMask(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	imported := f.importTemplate(t, "重叠提醒模板")

	maskOn := true
	saved, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "field", Page: 1, X: 100, Y: 100, W: 100, H: 20, Type: model.PlacementField, Field: "contract.contract_no"},
			{ID: "seal", Page: 1, X: 120, Y: 100, W: 100, H: 100, Type: model.PlacementSeal},
			{ID: "sig", Page: 1, X: 100, Y: 500, W: 120, H: 40, Type: model.PlacementSignature, Party: model.PlacementPartyB},
		},
		MaskFieldValues: &maskOn,
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	// Overlap never blocks the save: a new version is created with a warning.
	if saved.Template.Version != 2 {
		t.Fatalf("saved version = %d, want 2", saved.Template.Version)
	}
	if len(saved.Overlaps) != 1 {
		t.Fatalf("overlaps = %#v, want exactly one", saved.Overlaps)
	}
	overlap := saved.Overlaps[0]
	if overlap.A != "seal" || overlap.B != "field" || overlap.Ratio != 0.8 || overlap.Page != 1 {
		t.Fatalf("overlap = %+v", overlap)
	}
	if !strings.Contains(overlap.Message, "重叠") || !strings.Contains(overlap.Message, "公章") {
		t.Fatalf("overlap message = %q", overlap.Message)
	}
	foundSummary := false
	for _, warning := range saved.Warnings {
		if strings.Contains(warning, "重叠") && strings.Contains(warning, "已保存") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("missing overlap summary warning: %#v", saved.Warnings)
	}
	if !saved.Template.MaskFieldValues {
		t.Fatal("mask flag should be saved on the new version")
	}
	stored, err := f.rd.GetTemplate(ctx, saved.Template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.MaskFieldValues {
		t.Fatal("mask flag should round-trip through the store")
	}

	// Nil keeps the setting; an explicit false turns it off again.
	carried, err := f.svc.SaveTemplateMapping(ctx, saved.Template.ID, service.TemplateSaveInput{
		Placements: saved.Template.Placements,
	})
	if err != nil {
		t.Fatalf("save carrying mask: %v", err)
	}
	if !carried.Template.MaskFieldValues {
		t.Fatal("mask flag should carry over when not provided")
	}
	maskOff := false
	cleared, err := f.svc.SaveTemplateMapping(ctx, carried.Template.ID, service.TemplateSaveInput{
		Placements:      carried.Template.Placements,
		MaskFieldValues: &maskOff,
	})
	if err != nil {
		t.Fatalf("save clearing mask: %v", err)
	}
	if cleared.Template.MaskFieldValues {
		t.Fatal("mask flag should be cleared when explicitly false")
	}
	// With the flag on, preview must still render (white box is additive).
	if _, err := f.svc.PreviewTemplate(ctx, saved.Template.ID, service.TemplatePreviewInput{Sample: true}); err != nil {
		t.Fatalf("preview with mask: %v", err)
	}
}

// te-rccn0：模板体检不再对「相对方 / 年度」误报：只要用 party_b.* 或
// contract.signed_on 表达同样的信息，就不再提示必填未标定；没有这些绑定
// 时仍照常提示。
func TestTemplateCheckRequiredCoverRules(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	imported := f.importTemplate(t, "必填覆盖模板")

	covered, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "no", Page: 1, X: 60, Y: 100, W: 220, H: 20, Type: model.PlacementField, Field: "contract.contract_no"},
			{ID: "title", Page: 1, X: 60, Y: 130, W: 220, H: 20, Type: model.PlacementField, Field: "contract.title"},
			{ID: "party_name", Page: 1, X: 60, Y: 160, W: 220, H: 20, Type: model.PlacementField, Field: "party_b.name"},
			{ID: "signed", Page: 1, X: 60, Y: 190, W: 220, H: 20, Type: model.PlacementField, Field: "contract.signed_on"},
		},
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	check, err := f.svc.CheckTemplate(ctx, covered.Template.ID)
	if err != nil {
		t.Fatalf("check template: %v", err)
	}
	missing := map[string]bool{}
	for _, issue := range check.Issues {
		if issue.Code == "missing_required_field" {
			missing[issue.Field] = true
		}
	}
	if missing["contract.party_b"] {
		t.Fatalf("相对方 should be covered by party_b.name: %#v", check.Issues)
	}
	if missing["contract.year"] {
		t.Fatalf("年度 should be covered by contract.signed_on: %#v", check.Issues)
	}

	// The uncalibrated original version still reports both required fields.
	legacy, err := f.svc.CheckTemplate(ctx, imported.Template.ID)
	if err != nil {
		t.Fatalf("check uncalibrated template: %v", err)
	}
	legacyMissing := map[string]bool{}
	for _, issue := range legacy.Issues {
		if issue.Code == "missing_required_field" {
			legacyMissing[issue.Field] = true
		}
	}
	if !legacyMissing["contract.party_b"] || !legacyMissing["contract.year"] {
		t.Fatalf("uncalibrated template should still report 相对方/年度: %#v", legacy.Issues)
	}
}
