package store_test

import (
	"context"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

func TestMaxContractNumberSuffix(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	for _, number := range []string{
		"HT-2026-0001", "HT-2026-0007", "HT-2025-0099", "C-manual", "HT-2026-补录",
	} {
		contract := model.Contract{ContractNo: number, Title: "测试合同"}
		if err := st.PutContract(ctx, &contract); err != nil {
			t.Fatalf("put %s: %v", number, err)
		}
	}
	rd := mustReader(t, st)

	got, err := rd.MaxContractNumberSuffix(ctx, "HT-2026-")
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("suffix = %d, want 7", got)
	}

	got, err = rd.MaxContractNumberSuffix(ctx, "HT-2027-")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("empty-year suffix = %d, want 0", got)
	}
}
