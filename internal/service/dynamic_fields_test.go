package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// TestPartyDynamicFieldValidation mirrors the contract-side rules: unknown
// keys (including built-in core keys) are rejected, and required user fields
// must be provided on creation.
func TestPartyDynamicFieldValidation(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	required := true
	if _, err := svc.CreateField(ctx, service.FieldInput{
		Entity:   model.FieldEntityParty,
		Key:      "credit_code",
		Label:    "统一社会信用代码",
		Type:     model.FieldTypeText,
		Required: &required,
	}); err != nil {
		t.Fatalf("create field: %v", err)
	}

	_, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "张三", Data: map[string]any{"unknown": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "字段未定义") {
		t.Fatalf("unknown key should be rejected, got %v", err)
	}

	_, _, err = svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "张三", Data: map[string]any{model.FieldPartyName: "张三"},
	})
	if err == nil || !strings.Contains(err.Error(), "字段未定义") {
		t.Fatalf("built-in key in data should be rejected, got %v", err)
	}

	_, _, err = svc.SaveParty(ctx, service.PartyInput{Kind: model.PartyPerson, Name: "张三"})
	if err == nil || !strings.Contains(err.Error(), "必填") {
		t.Fatalf("required field should be enforced, got %v", err)
	}

	party, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "张三", Data: map[string]any{"credit_code": "91310000"},
	})
	if err != nil {
		t.Fatalf("valid party: %v", err)
	}
	if party.Data["credit_code"] != "91310000" {
		t.Fatalf("dynamic value not stored: %#v", party.Data)
	}
}
