package service_test

import (
	"context"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// te-thrth M5：模板引用了 contract.asset_* 占位符而合同未选房产时，预检必须
// 报 missing_asset_record 并阻断生成；补上房产后同一份合同通过预检。
func TestPreflightBlocksMissingAssetRecord(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	party := mustParty(t, f.svc, "张三")

	imported := f.importTemplate(t, "资产阻断模板")
	saved, err := f.svc.SaveTemplateMapping(ctx, imported.Template.ID, service.TemplateSaveInput{
		Placements: []model.Placement{
			{ID: "addr", Page: 1, X: 120, Y: 150, W: 200, H: 22,
				Type: model.PlacementField, Field: "contract.asset_address"},
		},
	})
	if err != nil {
		t.Fatalf("save mapping: %v", err)
	}
	tpl := saved.Template

	contract := mustContract(t, f.svc, service.ContractInput{
		Year: 2026, Title: "未选房产合同", PartyBID: party.ID, Amount: 1200,
	})
	preflight, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if preflight.Ready || preflight.Status != service.PreflightBlocked {
		t.Fatalf("preflight = %+v", preflight)
	}
	missing := false
	for _, issue := range preflight.Issues {
		if issue.Code == "missing_asset_record" {
			missing = true
		}
	}
	if !missing {
		t.Fatalf("preflight issues = %+v", preflight.Issues)
	}

	asset := mustAsset(t, f.svc, "幸福里 501")
	mustContract(t, f.svc, service.ContractInput{
		ID: contract.ID, ContractNo: contract.ContractNo, Year: contract.Year,
		Title: contract.Title, PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Amount: 1200,
	})
	after, err := f.svc.PreflightContractGeneration(ctx, tpl.ID, contract.ID)
	if err != nil {
		t.Fatalf("preflight after asset: %v", err)
	}
	for _, issue := range after.Issues {
		if issue.Code == "missing_asset_record" {
			t.Fatalf("issue remains after asset: %+v", after.Issues)
		}
	}
	if !after.Ready || after.Status != service.PreflightReady {
		t.Fatalf("preflight after asset = %+v", after)
	}
}
