package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// AssetInput is the editable payload for a property (asset) archive. AreaSqm
// nil clears the optional area on update; Enabled nil keeps the current switch
// (disable/enable is an explicit action, not an implicit form default).
type AssetInput struct {
	ID        string   `json:"id"`
	AssetType string   `json:"asset_type"`
	Name      string   `json:"name"`
	Address   string   `json:"address"`
	RoomNo    string   `json:"room_no"`
	AreaSqm   *float64 `json:"area_sqm"`
	Note      string   `json:"note"`
	Enabled   *bool    `json:"enabled"`
}

// assetFieldLabels 是资产档案变更记录的字段中文名。
var assetFieldLabels = map[string]string{
	"asset_type": "资产类型",
	"name":       "名称",
	"address":    "地址",
	"room_no":    "房号",
	"area_sqm":   "面积（㎡）",
	"note":       "备注",
	"enabled":    "启用状态",
}

// ListAssets returns property archives matching f.
func (s *Service) ListAssets(ctx context.Context, f model.ListFilter) (model.List[model.Asset], error) {
	return s.rd.ListAssets(ctx, f)
}

// SaveAsset creates or updates a property archive. It returns non-blocking
// warnings such as a duplicated archive name.
func (s *Service) SaveAsset(ctx context.Context, in AssetInput) (model.Asset, []string, error) {
	var existing *model.Asset
	if id := strings.TrimSpace(in.ID); id != "" {
		a, err := s.rd.GetAsset(ctx, id)
		if err != nil {
			if isNotFound(err) {
				return model.Asset{}, nil, business("not_found", "房产档案不存在")
			}
			return model.Asset{}, nil, err
		}
		existing = &a
	}
	asset, warnings, err := s.buildAsset(ctx, in, existing)
	if err != nil {
		return model.Asset{}, nil, err
	}
	if err := s.st.PutAsset(ctx, &asset); err != nil {
		return model.Asset{}, nil, err
	}
	// 重读持久化行：派生字段（如 lease_status）只在读取路径计算，保存响应
	// 与后续变更记录必须基于同一份数据，避免两条派生规则漂移。
	persisted, err := s.rd.GetAsset(ctx, asset.ID)
	if err != nil {
		return model.Asset{}, nil, fmt.Errorf("读取保存后的房产档案: %w", err)
	}
	asset = persisted
	action := model.ChangeActionCreate
	summary := fmt.Sprintf("新建房产档案「%s」", asset.Name)
	var changes []model.FieldChange
	if existing != nil {
		action = model.ChangeActionUpdate
		summary = fmt.Sprintf("修改房产档案「%s」", asset.Name)
		if existing.Enabled != asset.Enabled {
			if asset.Enabled {
				summary = fmt.Sprintf("启用房产档案「%s」", asset.Name)
			} else {
				summary = fmt.Sprintf("停用房产档案「%s」", asset.Name)
			}
		}
		changes = diffValueMaps(assetValues(existing), assetValues(&asset), assetFieldLabels)
	} else {
		changes = changesFromValues(assetValues(&asset), assetFieldLabels)
	}
	s.logChange(ctx, model.ChangeEntityAsset, asset.ID, action, summary, changes)
	return asset, warnings, nil
}

func (s *Service) buildAsset(ctx context.Context, in AssetInput, existing *model.Asset) (model.Asset, []string, error) {
	validation := &ValidationErrors{}
	asset := model.Asset{AssetType: model.AssetTypeHouse, Enabled: true}
	if existing != nil {
		asset = *existing
	}
	if assetType, ok := normalizeAssetType(in.AssetType); ok {
		asset.AssetType = assetType
	} else {
		validation.add("asset_type", "资产类型", "目前只支持 房产")
	}
	asset.Name = strings.TrimSpace(in.Name)
	if asset.Name == "" {
		validation.add("name", "名称", "不能为空")
	} else {
		asset.Name = truncateRunes(asset.Name, 120)
	}
	asset.Address = truncateRunes(strings.TrimSpace(in.Address), 300)
	asset.RoomNo = truncateRunes(strings.TrimSpace(in.RoomNo), 60)
	asset.Note = strings.TrimSpace(in.Note)
	if in.AreaSqm != nil {
		if *in.AreaSqm < 0 {
			validation.add("area_sqm", "面积", "不能为负数")
		} else {
			area := *in.AreaSqm
			asset.AreaSqm = &area
		}
	} else {
		// 表单始终提交面积：留空即清除（选填字段），与只更新非空字段的
		// 相对方不同，避免用户无法清空一个填错的面积。
		asset.AreaSqm = nil
	}
	if in.Enabled != nil {
		asset.Enabled = *in.Enabled
	}
	if err := validation.errOrNil(); err != nil {
		return model.Asset{}, nil, err
	}
	warnings := []string{}
	if asset.Name != "" {
		matches, err := s.rd.FindAssetsByName(ctx, asset.Name)
		if err != nil {
			return model.Asset{}, nil, err
		}
		for _, m := range matches {
			if existing != nil && m.ID == existing.ID {
				continue
			}
			warnings = append(warnings,
				fmt.Sprintf("档案中已存在同名房产「%s」（%s）；如非同一套，请在名称中区分",
					asset.Name, m.ID))
		}
	}
	return asset, warnings, nil
}

// DeleteAsset removes a property archive. Assets referenced by contracts will
// be protected once the contract→asset link lands; until then nothing can
// reference one, so deletion only needs an existence check.
func (s *Service) DeleteAsset(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	asset, err := s.rd.GetAsset(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return business("not_found", "房产档案不存在")
		}
		return err
	}
	if err := s.st.DeleteAsset(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, model.ChangeEntityAsset, id, model.ChangeActionDelete,
		fmt.Sprintf("删除房产档案「%s」", asset.Name), nil)
	return nil
}

// normalizeAssetType accepts house (the only v0.2 kind) and its empty default.
func normalizeAssetType(kind string) (string, bool) {
	switch strings.TrimSpace(kind) {
	case "", model.AssetTypeHouse:
		return model.AssetTypeHouse, true
	}
	return "", false
}

func assetTypeLabel(kind string) string {
	if kind == model.AssetTypeHouse {
		return "房产"
	}
	return kind
}

func assetValues(asset *model.Asset) map[string]string {
	area := ""
	if asset.AreaSqm != nil {
		area = strconv.FormatFloat(*asset.AreaSqm, 'f', -1, 64)
	}
	enabled := "否"
	if asset.Enabled {
		enabled = "是"
	}
	return map[string]string{
		"asset_type": assetTypeLabel(asset.AssetType),
		"name":       asset.Name,
		"address":    asset.Address,
		"room_no":    asset.RoomNo,
		"area_sqm":   area,
		"note":       asset.Note,
		"enabled":    enabled,
	}
}

// changesFromValues lists non-empty values in key order; used for create
// entries so the audit log shows what the new record started with.
func changesFromValues(values, labels map[string]string) []model.FieldChange {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changes := []model.FieldChange{}
	for _, key := range keys {
		if values[key] == "" {
			continue
		}
		changes = append(changes, model.FieldChange{
			Field: key, Label: labels[key], Old: "", New: values[key],
		})
	}
	return changes
}
