package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// PutFieldDefinition inserts or updates a field definition.
func (s *Store) PutFieldDefinition(ctx context.Context, f *model.FieldDefinition) error {
	if f == nil {
		return fmt.Errorf("field definition is nil")
	}
	if f.ID == "" {
		id, err := model.NewID("fld")
		if err != nil {
			return err
		}
		f.ID = id
	}
	if f.Key == "" || f.Label == "" || f.Type == "" {
		return fmt.Errorf("field key, label and type are required")
	}
	switch f.Entity {
	case model.FieldEntityContract, model.FieldEntityParty:
	default:
		return fmt.Errorf("invalid field entity %q", f.Entity)
	}
	now := model.Now()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	options, err := json.Marshal(nonNilStrings(f.Options))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO field_definitions (id, entity, key, label, type, required,
			default_value, options_json, in_ledger, in_template, built_in, archived,
			sort_order, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			entity = excluded.entity, key = excluded.key, label = excluded.label,
			type = excluded.type, required = excluded.required,
			default_value = excluded.default_value, options_json = excluded.options_json,
			in_ledger = excluded.in_ledger, in_template = excluded.in_template,
			built_in = excluded.built_in, archived = excluded.archived,
			sort_order = excluded.sort_order, note = excluded.note,
			updated_at = excluded.updated_at`,
		f.ID, f.Entity, f.Key, f.Label, f.Type, boolInt(f.Required), f.DefaultValue,
		string(options), boolInt(f.InLedger), boolInt(f.InTemplate), boolInt(f.BuiltIn),
		boolInt(f.Archived), f.SortOrder, f.Note,
		formatTime(f.CreatedAt), formatTime(f.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put field definition: %w", err)
	}
	return nil
}

// DeleteFieldDefinition removes a user-defined field definition. Data already
// stored under the key is intentionally kept in data_json.
func (s *Store) DeleteFieldDefinition(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM field_definitions WHERE id = ? AND built_in = 0`, id)
	if err != nil {
		return fmt.Errorf("delete field definition: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendChangeLog appends one audit entry.
func (s *Store) AppendChangeLog(ctx context.Context, entry *model.ChangeLog) error {
	if entry == nil {
		return fmt.Errorf("change log is nil")
	}
	if entry.ID == "" {
		id, err := model.NewID("chg")
		if err != nil {
			return err
		}
		entry.ID = id
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = model.Now()
	}
	changes, err := json.Marshal(nonNilChanges(entry.Changes))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO change_log (id, entity, entity_id, action, actor, summary, changes_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Entity, entry.EntityID, entry.Action, entry.Actor,
		entry.Summary, string(changes), formatTime(entry.CreatedAt))
	if err != nil {
		return fmt.Errorf("append change log: %w", err)
	}
	return nil
}

// DeleteParty removes a party row.
func (s *Store) DeleteParty(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM parties WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete party: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteContract removes a contract row. Output documents keep their record
// with contract_id set to NULL (see the schema foreign key).
func (s *Store) DeleteContract(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM contracts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete contract: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nonNilChanges(in []model.FieldChange) []model.FieldChange {
	if in == nil {
		return []model.FieldChange{}
	}
	return in
}
