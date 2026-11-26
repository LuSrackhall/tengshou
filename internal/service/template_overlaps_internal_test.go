package service

import (
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// 白盒：标定保存时的公章/签名与字段重叠提醒（te-rccn0）。比例以较小框面积
// 为基数；≤30% 不提示，跨页不提示；保存本身不受影响（由调用方保证）。
func TestPlacementOverlaps(t *testing.T) {
	seal := model.Placement{ID: "seal", Page: 1, X: 100, Y: 100, W: 100, H: 100, Type: model.PlacementSeal}
	cases := []struct {
		name  string
		list  []model.Placement
		want  int
		ids   [2]string
		ratio float64
	}{
		{
			name: "seal covers half of a field box",
			list: []model.Placement{
				seal,
				{ID: "field", Page: 1, X: 150, Y: 100, W: 100, H: 20, Type: model.PlacementField, Field: "contract.amount"},
			},
			want:  1,
			ids:   [2]string{"seal", "field"},
			ratio: 0.5,
		},
		{
			name: "field entirely inside a signature box",
			list: []model.Placement{
				{ID: "sig", Page: 1, X: 100, Y: 100, W: 120, H: 40, Type: model.PlacementSignature, Party: model.PlacementPartyB},
				{ID: "field", Page: 1, X: 110, Y: 105, W: 60, H: 20, Type: model.PlacementField, Field: "contract.amount"},
			},
			want:  1,
			ids:   [2]string{"sig", "field"},
			ratio: 1,
		},
		{
			name: "only 30 percent overlap is not reported",
			list: []model.Placement{
				seal,
				{ID: "field", Page: 1, X: 170, Y: 100, W: 100, H: 20, Type: model.PlacementField, Field: "contract.amount"},
			},
			want: 0,
		},
		{
			name: "boxes on different pages never overlap",
			list: []model.Placement{
				seal,
				{ID: "field", Page: 2, X: 100, Y: 100, W: 100, H: 20, Type: model.PlacementField, Field: "contract.amount"},
			},
			want: 0,
		},
		{
			name: "seal against seal is not reported",
			list: []model.Placement{
				seal,
				{ID: "seal2", Page: 1, X: 100, Y: 100, W: 100, H: 100, Type: model.PlacementSeal},
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := placementOverlaps(tc.list)
			if len(got) != tc.want {
				t.Fatalf("overlaps = %#v, want %d entries", got, tc.want)
			}
			if tc.want == 0 {
				return
			}
			if got[0].A != tc.ids[0] || got[0].B != tc.ids[1] {
				t.Fatalf("overlap ids = %s/%s, want %s/%s", got[0].A, got[0].B, tc.ids[0], tc.ids[1])
			}
			if got[0].Ratio != tc.ratio {
				t.Fatalf("ratio = %v, want %v", got[0].Ratio, tc.ratio)
			}
			if got[0].Page != 1 || got[0].Message == "" {
				t.Fatalf("overlap = %+v", got[0])
			}
		})
	}
}

// 白盒：必填字段的间接覆盖规则（te-rccn0）。相对方可用任意 party_b.* 绑定，
// 年度可由签订日期覆盖；未知键与未绑定键保持原提示。
func TestRequiredFieldCovered(t *testing.T) {
	bound := map[string]bool{
		"party_b.name":       true,
		"contract.signed_on": true,
	}
	if !requiredFieldCovered("contract.party_b", bound) {
		t.Fatal("party_b.* binding should cover 相对方")
	}
	if !requiredFieldCovered("contract.year", bound) {
		t.Fatal("contract.signed_on binding should cover 年度")
	}
	if requiredFieldCovered("contract.party_b", map[string]bool{"party_a.name": true}) {
		t.Fatal("party_a.* binding must not cover contract.party_b")
	}
	if requiredFieldCovered("contract.year", map[string]bool{"contract.valid_to": true}) {
		t.Fatal("unrelated contract binding must not cover 年度")
	}
	if requiredFieldCovered("contract.title", bound) {
		t.Fatal("field without a rule must not be covered")
	}
}
