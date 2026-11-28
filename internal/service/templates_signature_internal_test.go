package service

import (
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// 白盒：签名位置选中合同关联签名而不是档案旧行（te-nz58f）。
// 两个候选同一时间戳、旧行排在前面（模拟旧排序同秒并发），选中结果必须
// 与顺序无关；没有合同关联行时才回退到档案旧行。
func TestSignatureForPrefersContractLinkedRow(t *testing.T) {
	same := time.Date(2026, 11, 28, 3, 0, 0, 0, time.UTC)
	legacy := model.Signature{
		ID: "sig-000-legacy", PartyID: "pty_b", SignerName: "乙方",
		AssetPath: "assets/signatures/legacy.png", SHA256: "sha-legacy",
		CapturedAt: same, CreatedAt: same,
	}
	linked := model.Signature{
		ID: "sig-999-linked", PartyID: "pty_b", ContractID: "con_1", SignerName: "乙方",
		AssetPath: "assets/signatures/linked.png", SHA256: "sha-linked",
		Source: model.SignatureSourceArchive, CapturedAt: same, CreatedAt: same,
	}
	placement := model.Placement{Type: model.PlacementSignature, Party: model.PlacementPartyB}
	newRC := func(sigs ...model.Signature) *templateRenderContext {
		return &templateRenderContext{
			contract:   model.Contract{ID: "con_1"},
			partyA:     &model.Party{ID: "pty_a"},
			partyB:     &model.Party{ID: "pty_b"},
			signatures: sigs,
		}
	}

	sig, ok := newRC(legacy, linked).signatureFor(placement)
	if !ok || sig.ID != linked.ID {
		t.Fatalf("linked-first order picked %+v, ok=%v; want linked row", sig, ok)
	}
	sig, ok = newRC(linked, legacy).signatureFor(placement)
	if !ok || sig.ID != linked.ID {
		t.Fatalf("linked-last order picked %+v, ok=%v; want linked row", sig, ok)
	}

	sig, ok = newRC(legacy).signatureFor(placement)
	if !ok || sig.ID != legacy.ID {
		t.Fatalf("legacy fallback picked %+v, ok=%v; want legacy row", sig, ok)
	}

	if _, ok := newRC().signatureFor(placement); ok {
		t.Fatal("placement without any party signature must report missing")
	}
	// 甲方位置没有签名时不得借用乙方的记录。
	if _, ok := newRC(legacy, linked).signatureFor(model.Placement{
		Type: model.PlacementSignature, Party: model.PlacementPartyA,
	}); ok {
		t.Fatal("placement without a matching party must not fall back cross-party")
	}
}
