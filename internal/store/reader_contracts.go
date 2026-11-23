package store

import (
	"context"
	"strconv"
	"strings"
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
