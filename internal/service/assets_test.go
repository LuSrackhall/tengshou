package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func floatPtr(v float64) *float64 { return &v }

func TestAssetLifecycle(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()

	asset, warnings, err := svc.SaveAsset(ctx, service.AssetInput{
		Name: "幸福里 3 号院 2 单元 501", Address: "幸福路 3 号", RoomNo: "2-501",
		AreaSqm: floatPtr(89.5), Note: "临近地铁",
	})
	if err != nil {
		t.Fatalf("save asset: %v", err)
	}
	if asset.AssetType != model.AssetTypeHouse {
		t.Fatalf("asset_type = %q, want house", asset.AssetType)
	}
	if !asset.Enabled {
		t.Fatal("new asset should be enabled by default")
	}
	if asset.LeaseStatus != model.LeaseStatusVacant {
		t.Fatalf("lease_status = %q, want vacant", asset.LeaseStatus)
	}
	if asset.AreaSqm == nil || *asset.AreaSqm != 89.5 {
		t.Fatalf("area_sqm = %v, want 89.5", asset.AreaSqm)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	stored, err := rd.GetAsset(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != asset.Name || stored.RoomNo != "2-501" {
		t.Fatalf("stored asset mismatch: %#v", stored)
	}

	// 同名档案只给出非阻断提示。
	_, warnings, err = svc.SaveAsset(ctx, service.AssetInput{Name: asset.Name})
	if err != nil {
		t.Fatalf("save duplicate name: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "同名房产") {
		t.Fatalf("warnings = %v, want duplicate-name hint", warnings)
	}

	// 更新：留空面积即清除；未提交 enabled 时保持启用。
	updated, _, err := svc.SaveAsset(ctx, service.AssetInput{
		ID: asset.ID, Name: asset.Name, Address: "幸福路 3 号（北门）", RoomNo: "2-501",
	})
	if err != nil {
		t.Fatalf("update asset: %v", err)
	}
	if updated.AreaSqm != nil {
		t.Fatalf("area should be cleared, got %v", *updated.AreaSqm)
	}
	if !updated.Enabled {
		t.Fatal("enabled should be preserved when not submitted")
	}
	if updated.Address != "幸福路 3 号（北门）" {
		t.Fatalf("address = %q", updated.Address)
	}

	// 停用与启用都走 update，并在变更记录里给出可读摘要。
	disabled := false
	updated, _, err = svc.SaveAsset(ctx, service.AssetInput{
		ID: asset.ID, Name: asset.Name, Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("disable asset: %v", err)
	}
	if updated.Enabled {
		t.Fatal("asset should be disabled")
	}
	disabledList, err := svc.ListAssets(ctx, model.ListFilter{Status: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range disabledList.Items {
		if item.ID == asset.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled filter missed asset: %#v", disabledList.Items)
	}
	enabledList, err := svc.ListAssets(ctx, model.ListFilter{Status: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range enabledList.Items {
		if item.ID == asset.ID {
			t.Fatal("disabled asset must not appear in enabled filter")
		}
	}

	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntityAsset, EntityID: asset.ID})
	if err != nil {
		t.Fatal(err)
	}
	var summaries []string
	for _, entry := range logs.Items {
		summaries = append(summaries, entry.Summary)
	}
	joined := strings.Join(summaries, "\n")
	for _, want := range []string{"新建房产档案", "修改房产档案", "停用房产档案"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("change log missing %q: %s", want, joined)
		}
	}

	// 删除后不可再读取，并留下删除审计。
	if err := svc.DeleteAsset(ctx, asset.ID); err != nil {
		t.Fatalf("delete asset: %v", err)
	}
	if _, err := rd.GetAsset(ctx, asset.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted asset err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteAsset(ctx, asset.ID); err == nil {
		t.Fatal("deleting a missing asset should fail")
	}
	logs, err = rd.ListChangeLogs(ctx, store.ChangeFilter{Entity: model.ChangeEntityAsset, EntityID: asset.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Items[0].Action != model.ChangeActionDelete {
		t.Fatalf("latest action = %q, want delete", logs.Items[0].Action)
	}
}

func TestAssetValidation(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()

	_, _, err := svc.SaveAsset(ctx, service.AssetInput{Name: "   "})
	var validation *service.ValidationErrors
	if !errors.As(err, &validation) {
		t.Fatalf("empty name err = %v, want ValidationErrors", err)
	}
	if len(validation.Items) != 1 || validation.Items[0].Field != "name" {
		t.Fatalf("validation = %#v", validation.Items)
	}

	_, _, err = svc.SaveAsset(ctx, service.AssetInput{Name: "负面积", AreaSqm: floatPtr(-1)})
	if !errors.As(err, &validation) || validation.Items[0].Field != "area_sqm" {
		t.Fatalf("negative area err = %v", err)
	}

	_, _, err = svc.SaveAsset(ctx, service.AssetInput{Name: "车位", AssetType: "parking"})
	if !errors.As(err, &validation) || validation.Items[0].Field != "asset_type" {
		t.Fatalf("unsupported type err = %v", err)
	}

	_, _, err = svc.SaveAsset(ctx, service.AssetInput{ID: "ast_missing", Name: "不存在"})
	var businessErr *service.Error
	if !errors.As(err, &businessErr) || businessErr.Code != "not_found" {
		t.Fatalf("update missing err = %v, want not_found", err)
	}
}
