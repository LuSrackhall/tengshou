package service

import (
	"math"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// 白盒：金额中文大写转换（te-7c5v1）。覆盖 0、整数、角分、万元级长金额与
// 非法输入；输出同时是模板绑定 contract.*.words 的渲染结果。
func TestMoneyWordsConversion(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, ""},
		{0.004, ""},
		{1500, "人民币壹仟伍佰元整"},
		{1500.5, "人民币壹仟伍佰元伍角"},
		{1500.05, "人民币壹仟伍佰元零伍分"},
		{1500.55, "人民币壹仟伍佰元伍角伍分"},
		{0.5, "人民币伍角"},
		{0.05, "人民币伍分"},
		{10, "人民币壹拾元整"},
		{10001, "人民币壹万零壹元整"},
		{12500.34, "人民币壹万贰仟伍佰元叁角肆分"},
		{100200300, "人民币壹亿零贰拾万零叁佰元整"},
		{100000000, "人民币壹亿元整"},
		{123456789.9, "人民币壹亿贰仟叁佰肆拾伍万陆仟柒佰捌拾玖元玖角"},
		{math.NaN(), ""},
		{math.Inf(1), ""},
		{-1, ""},
		{1e16, ""},
	}
	for _, tc := range cases {
		if got := MoneyWords(tc.in); got != tc.want {
			t.Fatalf("MoneyWords(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 白盒：模板绑定 contract.amount.words 与 contract.data.<金额键>.words 的
// 取值——自动大写与手填覆盖的优先级、空值回落与未知字段的防御。
func TestTemplateMoneyWordsBindings(t *testing.T) {
	rc := &templateRenderContext{
		contract: model.Contract{Amount: 1500.5, Data: map[string]any{"rent": 1000.05}},
		contractFields: map[string]model.FieldDefinition{
			"rent": {Key: "rent", Label: "月租", Type: model.FieldTypeMoney},
			"note": {Key: "note", Label: "备注", Type: model.FieldTypeText},
		},
	}
	if got := rc.textValue("contract.amount.words"); got != "人民币壹仟伍佰元伍角" {
		t.Fatalf("amount.words = %q", got)
	}
	if got := rc.textValue("contract.data.rent.words"); got != "人民币壹仟元零伍分" {
		t.Fatalf("data.rent.words = %q", got)
	}
	// 手填覆盖优先于自动换算；清空覆盖回落自动。
	rc.contract.Data["__amount_words__"] = " 人民币手填金额 "
	rc.contract.Data["__words__rent"] = "人民币手填月租"
	if got := rc.textValue("contract.amount.words"); got != "人民币手填金额" {
		t.Fatalf("amount.words override = %q", got)
	}
	if got := rc.textValue("contract.data.rent.words"); got != "人民币手填月租" {
		t.Fatalf("data.rent.words override = %q", got)
	}
	delete(rc.contract.Data, "__amount_words__")
	delete(rc.contract.Data, "__words__rent")
	if got := rc.textValue("contract.amount.words"); got != "人民币壹仟伍佰元伍角" {
		t.Fatalf("amount.words after clear = %q", got)
	}
	// 0 金额、非法金额、未知/非金额字段与无值字段都渲染空串。
	rc.contract.Amount = 0
	if got := rc.textValue("contract.amount.words"); got != "" {
		t.Fatalf("zero amount words = %q", got)
	}
	rc.contract.Data["rent"] = "abc"
	if got := rc.textValue("contract.data.rent.words"); got != "" {
		t.Fatalf("illegal rent words = %q", got)
	}
	rc.contract.Data["note"] = "x"
	if got := rc.textValue("contract.data.note.words"); got != "" {
		t.Fatalf("non-money words = %q", got)
	}
	if got := rc.textValue("contract.data.missing.words"); got != "" {
		t.Fatalf("missing field words = %q", got)
	}
}

// 白盒：大写虚拟绑定的准入规则——只有合同金额字段（内置 amount 或动态
// money 字段）可以加 .words；相对方与非金额字段一律拒绝。
func TestValidateBindingMoneyWords(t *testing.T) {
	contractFields := map[string]model.FieldDefinition{
		"rent": {Key: "rent", Label: "月租", Type: model.FieldTypeMoney},
		"note": {Key: "note", Label: "备注", Type: model.FieldTypeText},
	}
	partyFields := map[string]model.FieldDefinition{
		"deposit": {Key: "deposit", Label: "押金", Type: model.FieldTypeMoney},
	}
	for _, key := range []string{"contract.amount.words", "contract.data.rent.words"} {
		if err := validateBinding(key, contractFields, partyFields); err != nil {
			t.Fatalf("%s rejected: %v", key, err)
		}
	}
	for _, key := range []string{
		"contract.title.words",
		"contract.data.note.words",
		"contract.data.missing.words",
		"party_a.data.deposit.words",
	} {
		if err := validateBinding(key, contractFields, partyFields); err == nil {
			t.Fatalf("%s should be rejected", key)
		}
	}
}
