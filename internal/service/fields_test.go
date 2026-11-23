package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// builtInField loads one seeded built-in definition and fails the test when the
// migration seed is missing or the field is unexpectedly editable.
func builtInField(t *testing.T, rd *store.Reader, entity, key string) model.FieldDefinition {
	t.Helper()
	field, err := rd.GetFieldByKey(context.Background(), entity, key)
	if err != nil {
		t.Fatalf("get built-in field %s/%s: %v", entity, key, err)
	}
	if !field.BuiltIn {
		t.Fatalf("field %s/%s is not built-in", entity, key)
	}
	return field
}

// requireFieldError asserts a service business error with the expected code
// and a user-facing message containing want.
func requireFieldError(t *testing.T, err error, wantCode, wantMessage string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected business error containing %q, got nil", wantMessage)
	}
	if code := businessCode(t, err); code != wantCode {
		t.Fatalf("error code = %q, want %q (err: %v)", code, wantCode, err)
	}
	if !strings.Contains(err.Error(), wantMessage) {
		t.Fatalf("error = %q, want message containing %q", err.Error(), wantMessage)
	}
}

// TestBuiltInFieldProtection pins the rules that keep core identity fields
// intact: type, key and bookkeeping flags cannot be changed and the field
// cannot be archived, while name/default stay editable.
func TestBuiltInFieldProtection(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()

	t.Run("type is immutable", func(t *testing.T) {
		field := builtInField(t, rd, model.FieldEntityContract, model.FieldType)
		_, err := svc.UpdateField(ctx, field.ID, service.FieldInput{
			Type: model.FieldTypeNumber,
		})
		requireFieldError(t, err, "invalid", "内置字段类型不可修改")
	})

	t.Run("key is immutable", func(t *testing.T) {
		field := builtInField(t, rd, model.FieldEntityContract, model.FieldContractNo)
		_, err := svc.UpdateField(ctx, field.ID, service.FieldInput{
			Key: "contract_no_v2",
		})
		requireFieldError(t, err, "invalid", "字段键创建后不可修改")
	})

	t.Run("required cannot be unset", func(t *testing.T) {
		title := builtInField(t, rd, model.FieldEntityContract, model.FieldTitle)
		_, err := svc.UpdateField(ctx, title.ID, service.FieldInput{
			Required: boolPtr(false),
		})
		requireFieldError(t, err, "invalid", "内置字段「合同名称」必须保持必填")

		partyName := builtInField(t, rd, model.FieldEntityParty, model.FieldPartyName)
		_, err = svc.UpdateField(ctx, partyName.ID, service.FieldInput{
			Required: boolPtr(false),
		})
		requireFieldError(t, err, "invalid", "内置字段「名称」必须保持必填")
	})

	t.Run("archive is rejected", func(t *testing.T) {
		field := builtInField(t, rd, model.FieldEntityContract, model.FieldType)
		_, err := svc.ArchiveField(ctx, field.ID)
		requireFieldError(t, err, "invalid", "内置字段不能归档")
	})

	t.Run("label and default stay editable", func(t *testing.T) {
		field := builtInField(t, rd, model.FieldEntityContract, model.FieldType)
		defaultValue := "租赁"
		updated, err := svc.UpdateField(ctx, field.ID, service.FieldInput{
			Label:        "合同类型（改）",
			DefaultValue: &defaultValue,
		})
		if err != nil {
			t.Fatalf("update built-in label/default: %v", err)
		}
		if updated.Label != "合同类型（改）" || updated.DefaultValue != "租赁" {
			t.Fatalf("updated field = %+v, want label/default applied", updated)
		}
		stored, err := rd.GetFieldDefinition(ctx, field.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Label != "合同类型（改）" || stored.DefaultValue != "租赁" {
			t.Fatalf("stored field = %+v, want persisted label/default", stored)
		}
	})
}

// TestRuntimeFieldArchiveRestoreAndOrder pins archive/restore visibility and
// the ascending sort_order rendering that the ledger and forms rely on.
func TestRuntimeFieldArchiveRestoreAndOrder(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()

	sortFirst, sortLast := 1, 5
	first, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "order_last", Label: "排序靠后",
		Type: model.FieldTypeText, SortOrder: &sortLast,
	})
	if err != nil {
		t.Fatalf("create first field: %v", err)
	}
	last, err := svc.CreateField(ctx, service.FieldInput{
		Entity: model.FieldEntityContract, Key: "order_first", Label: "排序靠前",
		Type: model.FieldTypeText, SortOrder: &sortFirst,
	})
	if err != nil {
		t.Fatalf("create second field: %v", err)
	}

	indexOf := func(fields []model.FieldDefinition, id string) int {
		for i, field := range fields {
			if field.ID == id {
				return i
			}
		}
		return -1
	}
	findField := func(fields []model.FieldDefinition, id string) (model.FieldDefinition, bool) {
		for _, field := range fields {
			if field.ID == id {
				return field, true
			}
		}
		return model.FieldDefinition{}, false
	}

	active, err := svc.ListFields(ctx, model.FieldEntityContract, false)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(active, last.ID) < 0 || indexOf(active, first.ID) < 0 {
		t.Fatalf("new runtime fields missing from active list: %+v", active)
	}
	if indexOf(active, last.ID) > indexOf(active, first.ID) {
		t.Fatalf("sort_order not ascending: order_first=%d order_last=%d",
			indexOf(active, last.ID), indexOf(active, first.ID))
	}

	// Archive hides the field from the active list but keeps it visible when
	// archived rows are requested.
	if _, err := svc.ArchiveField(ctx, first.ID); err != nil {
		t.Fatalf("archive field: %v", err)
	}
	active, err = svc.ListFields(ctx, model.FieldEntityContract, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findField(active, first.ID); ok {
		t.Fatal("archived field still listed as active")
	}
	all, err := svc.ListFields(ctx, model.FieldEntityContract, true)
	if err != nil {
		t.Fatal(err)
	}
	archivedField, ok := findField(all, first.ID)
	if !ok || !archivedField.Archived {
		t.Fatalf("archived field = %+v, visible=%v; want archived entry when included", archivedField, ok)
	}

	// Restoring returns the field to the active list with archived cleared.
	if _, err := svc.UpdateField(ctx, first.ID, service.FieldInput{
		Archived: boolPtr(false),
	}); err != nil {
		t.Fatalf("restore field: %v", err)
	}
	active, err = svc.ListFields(ctx, model.FieldEntityContract, false)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := findField(active, first.ID)
	if !ok {
		t.Fatal("restored field missing from active list")
	}
	if restored.Archived {
		t.Fatalf("restored field still archived: %+v", restored)
	}
}
