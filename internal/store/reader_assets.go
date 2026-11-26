package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

const assetColumns = `id, asset_type, name, address, room_no, area_sqm, note,
	enabled, created_at, updated_at`

func scanAsset(sc scanner) (model.Asset, error) {
	var a model.Asset
	var area sql.NullFloat64
	var enabled int
	var created, updated string
	if err := sc.Scan(&a.ID, &a.AssetType, &a.Name, &a.Address, &a.RoomNo, &area,
		&a.Note, &enabled, &created, &updated); err != nil {
		return a, err
	}
	if area.Valid {
		value := area.Float64
		a.AreaSqm = &value
	}
	a.Enabled = enabled != 0
	// 合同→资产联动在后续珠：当前还没有合同能引用资产，一律按空置派生。
	// 接入后这里换成「存在生效中且未终止的合同即为在租」的查询结果。
	a.LeaseStatus = model.LeaseStatusVacant
	a.CreatedAt, a.UpdatedAt = parseTime(created), parseTime(updated)
	return a, nil
}

// ListAssets returns assets matching f. Query matches name/address/room_no;
// Status limits to enabled or disabled records.
func (r *Reader) ListAssets(ctx context.Context, f model.ListFilter) (model.List[model.Asset], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(name LIKE ? OR address LIKE ? OR room_no LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	switch f.Status {
	case "enabled":
		where = append(where, "enabled = 1")
	case "disabled":
		where = append(where, "enabled = 0")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Asset]{}, err
	}
	query := `SELECT ` + assetColumns + ` FROM assets WHERE ` + clause +
		` ORDER BY name, id LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Asset]{}, err
	}
	defer rows.Close()
	items := []model.Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return model.List[model.Asset]{}, err
		}
		items = append(items, a)
	}
	return model.List[model.Asset]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

// GetAsset returns one asset by id.
func (r *Reader) GetAsset(ctx context.Context, id string) (model.Asset, error) {
	a, err := scanAsset(r.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM assets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Asset{}, ErrNotFound
	}
	return a, err
}

// FindAssetsByName returns assets whose trimmed name equals name.
func (r *Reader) FindAssetsByName(ctx context.Context, name string) ([]model.Asset, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE trim(name) = ? ORDER BY created_at, id`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
