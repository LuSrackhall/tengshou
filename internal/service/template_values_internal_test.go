package service

import (
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// 白盒：合同正文占位符对租期与房产字段的取值（te-thrth）。
func TestTemplateAssetAndLeasePlaceholderValues(t *testing.T) {
	area := 88.5
	rc := &templateRenderContext{
		contract: model.Contract{LeaseStart: "2026-02-01", LeaseEnd: "2027-01-31"},
		asset: &model.Asset{
			AssetType: model.AssetTypeHouse, Name: "幸福里 501",
			Address: "幸福路 1 号", RoomNo: "2 单元 501", AreaSqm: &area,
		},
	}
	cases := map[string]string{
		"contract.lease_start":   "2026年2月1日",
		"contract.lease_end":     "2027年1月31日",
		"contract.asset_name":    "幸福里 501",
		"contract.asset_address": "幸福路 1 号",
		"contract.asset_room_no": "2 单元 501",
		"contract.asset_area":    "88.5",
	}
	for key, want := range cases {
		if got := rc.textValue(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	// 未选房产时占位符返回空串，不 panic。
	rc.asset = nil
	for _, key := range []string{
		"contract.asset_name", "contract.asset_address",
		"contract.asset_room_no", "contract.asset_area",
	} {
		if got := rc.textValue(key); got != "" {
			t.Fatalf("%s without asset = %q", key, got)
		}
	}
}

func TestValidateBindingAcceptsAssetBuiltins(t *testing.T) {
	contractFields := map[string]model.FieldDefinition{}
	partyFields := map[string]model.FieldDefinition{}
	for _, key := range []string{
		"contract.lease_start", "contract.lease_end", "contract.asset_name",
		"contract.asset_address", "contract.asset_room_no", "contract.asset_area",
	} {
		if err := validateBinding(key, contractFields, partyFields); err != nil {
			t.Fatalf("%s rejected: %v", key, err)
		}
	}
}
