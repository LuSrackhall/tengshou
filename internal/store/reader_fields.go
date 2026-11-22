package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// ChangeFilter selects change-log rows.
type ChangeFilter struct {
	Entity   string
	EntityID string
	Limit    int
	Offset   int
}

// Normalize clamps pagination to safe bounds.
func (f *ChangeFilter) Normalize() {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
}

func scanFieldDefinition(sc scanner) (model.FieldDefinition, error) {
	var f model.FieldDefinition
	var required, inLedger, inTemplate, builtIn, archived int
	var optionsJSON, createdAt, updatedAt string
	if err := sc.Scan(&f.ID, &f.Entity, &f.Key, &f.Label, &f.Type, &required,
		&f.DefaultValue, &optionsJSON, &inLedger, &inTemplate, &builtIn, &archived,
		&f.SortOrder, &f.Note, &createdAt, &updatedAt); err != nil {
		return f, err
	}
	f.Required = required != 0
	f.InLedger = inLedger != 0
	f.InTemplate = inTemplate != 0
	f.BuiltIn = builtIn != 0
	f.Archived = archived != 0
	f.Options = parseJSONStrings(optionsJSON)
	f.CreatedAt, f.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return f, nil
}

const fieldDefinitionColumns = `id, entity, key, label, type, required, default_value,
	options_json, in_ledger, in_template, built_in, archived, sort_order, note,
	created_at, updated_at`

// ListFieldDefinitions returns definitions for one entity in render order.
func (r *Reader) ListFieldDefinitions(ctx context.Context, entity string, includeArchived bool) ([]model.FieldDefinition, error) {
	where := `entity = ?`
	args := []any{entity}
	if !includeArchived {
		where += ` AND archived = 0`
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+fieldDefinitionColumns+` FROM field_definitions WHERE `+where+
			` ORDER BY sort_order, built_in DESC, key`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FieldDefinition{}
	for rows.Next() {
		f, err := scanFieldDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFieldDefinition returns one field definition by id.
func (r *Reader) GetFieldDefinition(ctx context.Context, id string) (model.FieldDefinition, error) {
	f, err := scanFieldDefinition(r.db.QueryRowContext(ctx,
		`SELECT `+fieldDefinitionColumns+` FROM field_definitions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.FieldDefinition{}, ErrNotFound
	}
	return f, err
}

// GetFieldByKey returns one field definition by entity and key.
func (r *Reader) GetFieldByKey(ctx context.Context, entity, key string) (model.FieldDefinition, error) {
	f, err := scanFieldDefinition(r.db.QueryRowContext(ctx,
		`SELECT `+fieldDefinitionColumns+` FROM field_definitions WHERE entity = ? AND key = ?`,
		entity, key))
	if errors.Is(err, sql.ErrNoRows) {
		return model.FieldDefinition{}, ErrNotFound
	}
	return f, err
}

func scanChangeLog(sc scanner) (model.ChangeLog, error) {
	var e model.ChangeLog
	var changesJSON, createdAt string
	if err := sc.Scan(&e.ID, &e.Entity, &e.EntityID, &e.Action, &e.Actor, &e.Summary,
		&changesJSON, &createdAt); err != nil {
		return e, err
	}
	e.Changes = []model.FieldChange{}
	if strings.TrimSpace(changesJSON) != "" {
		_ = json.Unmarshal([]byte(changesJSON), &e.Changes)
		if e.Changes == nil {
			e.Changes = []model.FieldChange{}
		}
	}
	e.CreatedAt = parseTime(createdAt)
	return e, nil
}

// ListChangeLogs returns audit entries, newest first.
func (r *Reader) ListChangeLogs(ctx context.Context, f ChangeFilter) (model.List[model.ChangeLog], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if entity := strings.TrimSpace(f.Entity); entity != "" {
		where = append(where, "entity = ?")
		args = append(args, entity)
	}
	if entityID := strings.TrimSpace(f.EntityID); entityID != "" {
		where = append(where, "entity_id = ?")
		args = append(args, entityID)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_log WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.ChangeLog]{}, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, entity, entity_id, action, actor, summary, changes_json, created_at
		 FROM change_log WHERE `+clause+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.ChangeLog]{}, err
	}
	defer rows.Close()
	items := []model.ChangeLog{}
	for rows.Next() {
		e, err := scanChangeLog(rows)
		if err != nil {
			return model.List[model.ChangeLog]{}, err
		}
		items = append(items, e)
	}
	return model.List[model.ChangeLog]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}
