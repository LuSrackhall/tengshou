package store

import (
	"context"
	"fmt"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// PutAsset inserts or updates a property archive row. AreaSqm nil stores SQL
// NULL so an unset area stays distinguishable from zero.
func (s *Store) PutAsset(ctx context.Context, a *model.Asset) error {
	if a == nil {
		return fmt.Errorf("asset is nil")
	}
	if a.ID == "" {
		id, err := model.NewID("ast")
		if err != nil {
			return err
		}
		a.ID = id
	}
	if a.AssetType == "" {
		a.AssetType = model.AssetTypeHouse
	}
	now := model.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	var area any
	if a.AreaSqm != nil {
		area = *a.AreaSqm
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO assets (id, asset_type, name, address, room_no, area_sqm, note,
			enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			asset_type = excluded.asset_type, name = excluded.name,
			address = excluded.address, room_no = excluded.room_no,
			area_sqm = excluded.area_sqm, note = excluded.note,
			enabled = excluded.enabled, updated_at = excluded.updated_at`,
		a.ID, a.AssetType, a.Name, a.Address, a.RoomNo, area, a.Note,
		boolInt(a.Enabled), formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put asset: %w", err)
	}
	return nil
}

// DeleteAsset removes an asset row.
func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
