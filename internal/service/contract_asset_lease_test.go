package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

func mustAsset(t *testing.T, svc *service.Service, name string) model.Asset {
	t.Helper()
	asset, _, err := svc.SaveAsset(context.Background(), service.AssetInput{
		Name: name, Address: "幸福路 1 号", RoomNo: "501",
	})
	if err != nil {
		t.Fatalf("save asset: %v", err)
	}
	return asset
}

func TestContractAssetLeaseFlow(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	asset := mustAsset(t, svc, "幸福里 501")

	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "幸福里房屋租赁合同", PartyBID: party.ID,
		AssetID: asset.ID, LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31",
		Status: "生效",
	})
	if contract.AssetID != asset.ID || contract.LeaseStart != "2026-01-01" ||
		contract.LeaseEnd != "2026-12-31" {
		t.Fatalf("合同租期/房产未保存: %+v", contract)
	}
	stored, err := rd.GetContract(ctx, contract.ID)
	if err != nil || stored.AssetID != asset.ID || stored.LeaseEnd != "2026-12-31" {
		t.Fatalf("读回合同 = %+v err=%v", stored, err)
	}
	// 生效中合同让房产派生为在租；到期日参与占用判断的是状态，不是日期。
	rented, err := rd.GetAsset(ctx, asset.ID)
	if err != nil || rented.LeaseStatus != model.LeaseStatusRented {
		t.Fatalf("房产状态 = %q err=%v", rented.LeaseStatus, err)
	}

	// 租期顺序与房产存在性都在保存时校验。
	_, _, err = svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "倒序租期", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-12-01", LeaseEnd: "2026-01-01",
	})
	var validation *service.ValidationErrors
	if !errors.As(err, &validation) || len(validation.Items) == 0 ||
		validation.Items[0].Field != model.FieldLeaseEnd {
		t.Fatalf("倒序租期应报租赁到期日错误: %v", err)
	}
	_, _, err = svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "不存在的房产", PartyBID: party.ID, AssetID: "missing",
	})
	if !errors.As(err, &validation) || validation.Items[0].Field != model.FieldAssetID {
		t.Fatalf("不存在房产应报字段错误: %v", err)
	}

	// 重叠只提示不阻断，且提示里带冲突合同编号。
	other, warnings, err := svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "同房产新合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-06-01", LeaseEnd: "2027-05-31", Status: "草稿",
	})
	if err != nil {
		t.Fatalf("save overlapping contract: %v", err)
	}
	joined := strings.Join(warnings, "；")
	if !strings.Contains(joined, "租期与合同 "+contract.ContractNo+" 重叠") ||
		!strings.Contains(joined, "生效中的合同") {
		t.Fatalf("缺租期/占用提示: %#v", warnings)
	}

	// 续签草稿沿用房产，但清空租期，重新约定。
	draft, err := svc.RenewalDraft(ctx, other.ID)
	if err != nil {
		t.Fatalf("renewal draft: %v", err)
	}
	if draft.Input.AssetID != asset.ID || draft.Input.LeaseStart != "" || draft.Input.LeaseEnd != "" {
		t.Fatalf("续签草稿房产/租期 = %+v", draft.Input)
	}
	found := false
	for _, key := range draft.Reused {
		if key == model.FieldAssetID {
			found = true
		}
	}
	if !found {
		t.Fatalf("续签复用清单缺 %s: %#v", model.FieldAssetID, draft.Reused)
	}

	// 被合同引用的房产不能删除。
	if err := svc.DeleteAsset(ctx, asset.ID); err == nil ||
		!strings.Contains(err.Error(), "不能删除") {
		t.Fatalf("删除被引用房产应拒绝: %v", err)
	}

	// 合同归档后释放房产（到期本身不会释放）。
	mustContract(t, svc, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: contract.Title, PartyBID: party.ID,
		AssetID: asset.ID, LeaseStart: contract.LeaseStart, LeaseEnd: contract.LeaseEnd,
		Status: "归档",
	})
	mustContract(t, svc, service.ContractInput{
		ID: other.ID, Year: 2026, Title: other.Title, PartyBID: party.ID,
		AssetID: asset.ID, LeaseStart: other.LeaseStart, LeaseEnd: other.LeaseEnd,
		Status: "归档",
	})
	vacant, err := rd.GetAsset(ctx, asset.ID)
	if err != nil || vacant.LeaseStatus != model.LeaseStatusVacant {
		t.Fatalf("归档后房产状态 = %q err=%v", vacant.LeaseStatus, err)
	}
}

// TestContentSHAKeepsLegacyDigest pins the v0.2 digest rule: historical
// contracts with empty asset/lease fields hash exactly as they did before the
// fields existed, so retained signatures are not marked stale.
func TestContentSHAKeepsLegacyDigest(t *testing.T) {
	c := model.Contract{
		ID: "c1", ContractNo: "HT-2026-0001", Year: 2026, Title: "租赁合同",
		Type: "房屋租赁", PartyAID: "a", PartyBID: "b", Status: model.ContractActive,
		SignedOn: "2026-01-01", ValidFrom: "2026-02-01", ValidTo: "2027-01-31",
		Amount: 12000, Data: map[string]any{"rent": 1200.0},
	}
	legacy := struct {
		ID         string         `json:"id"`
		ContractNo string         `json:"contract_no"`
		Year       int            `json:"year"`
		Title      string         `json:"title"`
		Type       string         `json:"type"`
		PartyAID   string         `json:"party_a_id"`
		PartyBID   string         `json:"party_b_id"`
		Status     string         `json:"status"`
		SignedOn   string         `json:"signed_on"`
		ValidFrom  string         `json:"valid_from"`
		ValidTo    string         `json:"valid_to"`
		Amount     float64        `json:"amount"`
		Data       map[string]any `json:"data"`
	}{
		ID: c.ID, ContractNo: c.ContractNo, Year: c.Year, Title: c.Title,
		Type: c.Type, PartyAID: c.PartyAID, PartyBID: c.PartyBID, Status: c.Status,
		SignedOn: c.SignedOn, ValidFrom: c.ValidFrom, ValidTo: c.ValidTo,
		Amount: c.Amount, Data: c.Data,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	got, err := service.ContentSHA256(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatal("v0.2 fields changed the digest of a historical contract")
	}
	c.AssetID = "asset-1"
	c.LeaseStart = "2026-02-01"
	if changed, _ := service.ContentSHA256(c); changed == got {
		t.Fatal("asset/lease values must change the content digest")
	}
}
