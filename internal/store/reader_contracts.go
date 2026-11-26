package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// MaxContractNumberSuffix returns the highest numeric suffix among contract
// numbers that start with prefix, for example "HT-2026-". Numbers whose
// suffix is not a plain integer are ignored.
func (r *Reader) MaxContractNumberSuffix(ctx context.Context, prefix string) (int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT contract_no FROM contracts WHERE contract_no LIKE ?`, prefix+"%")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	maxSuffix := 0
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return 0, err
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(number, prefix)); err == nil && n > maxSuffix {
			maxSuffix = n
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return maxSuffix, nil
}

// ContractsForAsset returns non-archived contracts that reference one asset,
// optionally excluding one contract (the record being saved). The service uses
// it for the non-blocking occupancy hint; the hard overlap validation lands in
// a later bead. Archived contracts are excluded because they no longer occupy
// the asset.
func (r *Reader) ContractsForAsset(ctx context.Context, assetID, excludeID string) ([]model.Contract, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return []model.Contract{}, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+contractColumns+` FROM contracts
		 WHERE asset_id = ? AND id <> ? AND status <> ?
		 ORDER BY lease_start, contract_no LIMIT 50`,
		assetID, excludeID, model.ContractArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Contract{}
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountContractsByAsset returns how many contracts reference an asset,
// including archived ones, so deletion cannot orphan history.
func (r *Reader) CountContractsByAsset(ctx context.Context, assetID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM contracts WHERE asset_id = ?`, strings.TrimSpace(assetID)).Scan(&count)
	return count, err
}
