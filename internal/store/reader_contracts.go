package store

import (
	"context"
	"strconv"
	"strings"
	"time"

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
// it for the occupancy validation (active, non-terminated overlapping leases
// block saving) and for non-blocking hints. Archived contracts are excluded
// because they no longer occupy the asset; terminated contracts are returned
// so callers can distinguish them from active conflicts.
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

// CountContractLeaseExpiry counts active contracts whose lease ends within
// [today, cutoff] (expiring) and whose lease ended before today (expired).
// Rows without a lease end date are skipped because they cannot be classified.
// The dates are YYYY-MM-DD and compared as strings, matching stored values.
func (r *Reader) CountContractLeaseExpiry(ctx context.Context, today, cutoff string) (expiring, expired int, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN lease_end >= ? AND lease_end <= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN lease_end < ? THEN 1 ELSE 0 END), 0)
		 FROM contracts
		 WHERE status = ? AND lease_end <> ''`,
		today, cutoff, today, model.ContractActive).Scan(&expiring, &expired)
	return expiring, expired, err
}

// deriveExpiryState labels an active contract whose lease is about to end or
// has already ended. The label is a read-time reminder only: expiry never
// changes the contract status and never releases the asset (te-l0hh0 D4).
func deriveExpiryState(status, leaseEnd string, now time.Time) string {
	if status != model.ContractActive || strings.TrimSpace(leaseEnd) == "" {
		return ""
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(leaseEnd))
	if err != nil {
		return ""
	}
	today, err := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if err != nil {
		return ""
	}
	days := int(end.Sub(today).Hours() / 24)
	switch {
	case days < 0:
		return model.ExpiryStateExpired
	case days <= model.ExpiryReminderDays:
		return model.ExpiryStateExpiring
	default:
		return ""
	}
}

// CountContractsByAsset returns how many contracts reference an asset,
// including archived ones, so deletion cannot orphan history.
func (r *Reader) CountContractsByAsset(ctx context.Context, assetID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM contracts WHERE asset_id = ?`, strings.TrimSpace(assetID)).Scan(&count)
	return count, err
}
