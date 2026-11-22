package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// Reader is a read-only SQLite connection. Every method only issues SELECTs;
// the underlying connection additionally enforces PRAGMA query_only.
type Reader struct {
	db *sql.DB
}

// Close closes the read-only connection.
func (r *Reader) Close() error { return r.db.Close() }

// DB exposes the underlying read-only handle for diagnostics and tests.
func (r *Reader) DB() *sql.DB { return r.db }

type scanner interface {
	Scan(dest ...any) error
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func parseJSONMap(raw string) map[string]any {
	m := map[string]any{}
	if raw == "" {
		return m
	}
	_ = json.Unmarshal([]byte(raw), &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func parseJSONStrings(raw string) []string {
	var out []string
	if raw == "" {
		return []string{}
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func scanParty(sc scanner) (model.Party, error) {
	var p model.Party
	var kind, name, idNumber, phone, address, note, dataJSON, createdAt, updatedAt string
	if err := sc.Scan(&p.ID, &kind, &name, &idNumber, &phone, &address, &note,
		&dataJSON, &createdAt, &updatedAt); err != nil {
		return p, err
	}
	p.Kind, p.Name, p.IDNumber, p.Phone, p.Address, p.Note = kind, name, idNumber, phone, address, note
	p.Data = parseJSONMap(dataJSON)
	p.CreatedAt, p.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return p, nil
}

func scanContract(sc scanner) (model.Contract, error) {
	var c model.Contract
	var templateID, partyAID, partyBID sql.NullString
	var contractNo, title, ctype, status, signedOn, validFrom, validTo, dataJSON, createdAt, updatedAt string
	if err := sc.Scan(&c.ID, &contractNo, &c.Year, &title, &ctype, &templateID,
		&partyAID, &partyBID, &status, &signedOn, &validFrom, &validTo, &c.Amount,
		&dataJSON, &createdAt, &updatedAt); err != nil {
		return c, err
	}
	c.ContractNo, c.Title, c.Type, c.Status = contractNo, title, ctype, status
	c.TemplateID, c.PartyAID, c.PartyBID = templateID.String, partyAID.String, partyBID.String
	c.SignedOn, c.ValidFrom, c.ValidTo = signedOn, validFrom, validTo
	c.Data = parseJSONMap(dataJSON)
	c.CreatedAt, c.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return c, nil
}

const contractColumns = `id, contract_no, year, title, type, template_id, party_a_id,
	party_b_id, status, signed_on, valid_from, valid_to, amount, data_json,
	created_at, updated_at`

func scanSignature(sc scanner) (model.Signature, error) {
	var sig model.Signature
	var partyID, contractID sql.NullString
	var signerName, assetPath, sha, contentSHA, source, sessionID string
	var w, h int
	var capturedAt, createdAt string
	if err := sc.Scan(&sig.ID, &partyID, &contractID, &signerName, &assetPath, &sha,
		&contentSHA, &source, &sessionID, &w, &h, &capturedAt, &createdAt); err != nil {
		return sig, err
	}
	sig.PartyID, sig.ContractID = partyID.String, contractID.String
	sig.SignerName, sig.AssetPath, sig.SHA256 = signerName, assetPath, sha
	sig.ContentSHA256, sig.Source, sig.SessionID = contentSHA, source, sessionID
	sig.WidthPx, sig.HeightPx = w, h
	sig.CapturedAt, sig.CreatedAt = parseTime(capturedAt), parseTime(createdAt)
	return sig, nil
}

const signatureColumns = `id, party_id, contract_id, signer_name, asset_path, sha256,
	content_sha256, source, session_id, width_px, height_px, captured_at, created_at`

func scanDocument(sc scanner) (model.OutputDocument, error) {
	var d model.OutputDocument
	var contractID, templateID sql.NullString
	var filePath, sha string
	var pageCount int
	var createdAt string
	if err := sc.Scan(&d.ID, &contractID, &templateID, &d.TemplateVersion,
		&filePath, &sha, &pageCount, &createdAt); err != nil {
		return d, err
	}
	d.ContractID, d.TemplateID = contractID.String, templateID.String
	d.FilePath, d.SHA256, d.PageCount = filePath, sha, pageCount
	d.CreatedAt = parseTime(createdAt)
	return d, nil
}

const documentColumns = `id, contract_id, template_id, template_version,
	file_path, sha256, page_count, created_at`

// ListParties returns parties matching f.
func (r *Reader) ListParties(ctx context.Context, f model.ListFilter) (model.List[model.Party], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(name LIKE ? OR id_number LIKE ? OR phone LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM parties WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Party]{}, err
	}
	query := `SELECT ` + partyColumns + ` FROM parties WHERE ` + clause + ` ORDER BY name, id LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Party]{}, err
	}
	defer rows.Close()
	items := []model.Party{}
	for rows.Next() {
		p, err := scanParty(rows)
		if err != nil {
			return model.List[model.Party]{}, err
		}
		items = append(items, p)
	}
	return model.List[model.Party]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

const partyColumns = `id, kind, name, id_number, phone, address, note,
	data_json, created_at, updated_at`

// GetParty returns one party by id.
func (r *Reader) GetParty(ctx context.Context, id string) (model.Party, error) {
	p, err := scanParty(r.db.QueryRowContext(ctx, `SELECT `+partyColumns+` FROM parties WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Party{}, ErrNotFound
	}
	return p, err
}

// ListContracts returns contracts matching f. Year 0 means all years, so the
// same query backs both the annual view and cross-year reports.
func (r *Reader) ListContracts(ctx context.Context, f model.ListFilter) (model.List[model.Contract], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `(contract_no LIKE ? OR title LIKE ? OR party_b_id IN
			(SELECT id FROM parties WHERE name LIKE ?))`)
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Year > 0 {
		where = append(where, "year = ?")
		args = append(args, f.Year)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contracts WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Contract]{}, err
	}
	query := `SELECT ` + contractColumns + ` FROM contracts WHERE ` + clause +
		` ORDER BY year DESC, contract_no, created_at DESC, id LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Contract]{}, err
	}
	defer rows.Close()
	items := []model.Contract{}
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return model.List[model.Contract]{}, err
		}
		items = append(items, c)
	}
	return model.List[model.Contract]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

// GetContract returns one contract by id.
func (r *Reader) GetContract(ctx context.Context, id string) (model.Contract, error) {
	c, err := scanContract(r.db.QueryRowContext(ctx, `SELECT `+contractColumns+` FROM contracts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Contract{}, ErrNotFound
	}
	return c, err
}

// GetContractByNo returns one contract by its stable contract number.
func (r *Reader) GetContractByNo(ctx context.Context, number string) (model.Contract, error) {
	c, err := scanContract(r.db.QueryRowContext(ctx, `SELECT `+contractColumns+` FROM contracts WHERE contract_no = ?`, number))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Contract{}, ErrNotFound
	}
	return c, err
}

// ContractYears returns per-year contract counts, newest first.
func (r *Reader) ContractYears(ctx context.Context) ([]model.YearCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT year, COUNT(*) FROM contracts GROUP BY year ORDER BY year DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.YearCount{}
	for rows.Next() {
		var yc model.YearCount
		if err := rows.Scan(&yc.Year, &yc.Count); err != nil {
			return nil, err
		}
		out = append(out, yc)
	}
	return out, rows.Err()
}

// CountContractsForParty counts contracts that reference the party on either side.
func (r *Reader) CountContractsForParty(ctx context.Context, partyID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM contracts WHERE party_a_id = ? OR party_b_id = ?`,
		partyID, partyID).Scan(&n)
	return n, err
}

// FindSimilarContracts returns contracts with the same counterparty, year and
// title; used for non-blocking duplicate warnings.
func (r *Reader) FindSimilarContracts(ctx context.Context, partyBID string, year int, title, excludeID string) ([]model.Contract, error) {
	if partyBID == "" || year == 0 || strings.TrimSpace(title) == "" {
		return []model.Contract{}, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+contractColumns+` FROM contracts
		 WHERE party_b_id = ? AND year = ? AND title = ? AND id <> ?
		 ORDER BY contract_no LIMIT 20`, partyBID, year, title, excludeID)
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

// FindPartiesByName returns parties whose trimmed name equals name.
func (r *Reader) FindPartiesByName(ctx context.Context, name string) ([]model.Party, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+partyColumns+` FROM parties WHERE trim(name) = ? ORDER BY created_at, id`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Party{}
	for rows.Next() {
		p, err := scanParty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetContractDetail bundles a contract with parties, signatures and outputs.
func (r *Reader) GetContractDetail(ctx context.Context, id string) (model.ContractDetail, error) {
	c, err := r.GetContract(ctx, id)
	if err != nil {
		return model.ContractDetail{}, err
	}
	detail := model.ContractDetail{Contract: c, Signatures: []model.Signature{}, Documents: []model.OutputDocument{}}
	if c.PartyAID != "" {
		if p, err := r.GetParty(ctx, c.PartyAID); err == nil {
			detail.PartyA = &p
		} else if !errors.Is(err, ErrNotFound) {
			return detail, err
		}
	}
	if c.PartyBID != "" {
		if p, err := r.GetParty(ctx, c.PartyBID); err == nil {
			detail.PartyB = &p
		} else if !errors.Is(err, ErrNotFound) {
			return detail, err
		}
	}
	if c.TemplateID != "" {
		if t, err := r.GetTemplate(ctx, c.TemplateID); err == nil {
			t.HTML = ""
			detail.Template = &t
		} else if !errors.Is(err, ErrNotFound) {
			return detail, err
		}
	}
	sigs, err := r.listSignaturesForContract(ctx, c.ID, c.PartyAID, c.PartyBID)
	if err != nil {
		return detail, err
	}
	detail.Signatures = sigs
	docs, err := r.listDocumentsForContract(ctx, c.ID)
	if err != nil {
		return detail, err
	}
	detail.Documents = docs
	return detail, nil
}

// listSignaturesForContract returns signatures explicitly linked to the
// contract plus legacy party signatures that predate contract linking (B1/B2
// rows have no contract_id).
func (r *Reader) listSignaturesForContract(ctx context.Context, contractID string, partyIDs ...string) ([]model.Signature, error) {
	ids := []string{}
	for _, id := range partyIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	where := "contract_id = ?"
	args := []any{contractID}
	if contractID == "" {
		where = "1=0"
		args = nil
	}
	if len(ids) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		where += " OR (contract_id IS NULL AND source = '' AND party_id IN (" + placeholders + "))"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+signatureColumns+` FROM signature_assets WHERE `+where+`
		 ORDER BY captured_at DESC, created_at DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Signature{}
	for rows.Next() {
		sig, err := scanSignature(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

func (r *Reader) listDocumentsForContract(ctx context.Context, contractID string) ([]model.OutputDocument, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+documentColumns+` FROM output_documents WHERE contract_id = ?
		 ORDER BY created_at DESC, id`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.OutputDocument{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListTemplates returns template metadata (HTML omitted).
func (r *Reader) ListTemplates(ctx context.Context, f model.ListFilter) (model.List[model.Template], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+q+"%")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM templates WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Template]{}, err
	}
	query := `SELECT id, name, version, '', variables_json, is_active, created_at, updated_at
		FROM templates WHERE ` + clause + ` ORDER BY name, version DESC LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Template]{}, err
	}
	defer rows.Close()
	items := []model.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return model.List[model.Template]{}, err
		}
		items = append(items, t)
	}
	return model.List[model.Template]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

func scanTemplate(sc scanner) (model.Template, error) {
	var t model.Template
	var variablesJSON, createdAt, updatedAt string
	var active int
	if err := sc.Scan(&t.ID, &t.Name, &t.Version, &t.HTML, &variablesJSON, &active,
		&createdAt, &updatedAt); err != nil {
		return t, err
	}
	t.Variables = parseJSONStrings(variablesJSON)
	t.IsActive = active != 0
	t.CreatedAt, t.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return t, nil
}

// GetTemplate returns one template including its HTML.
func (r *Reader) GetTemplate(ctx context.Context, id string) (model.Template, error) {
	t, err := scanTemplate(r.db.QueryRowContext(ctx, `SELECT id, name, version, html, variables_json,
		is_active, created_at, updated_at FROM templates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Template{}, ErrNotFound
	}
	return t, err
}

// ListSignatures returns signature metadata.
func (r *Reader) ListSignatures(ctx context.Context, f model.ListFilter) (model.List[model.Signature], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(signer_name LIKE ? OR party_id LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	if contractID := strings.TrimSpace(f.ContractID); contractID != "" {
		where = append(where, "contract_id = ?")
		args = append(args, contractID)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM signature_assets WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Signature]{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+signatureColumns+` FROM signature_assets WHERE `+clause+
		` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Signature]{}, err
	}
	defer rows.Close()
	items := []model.Signature{}
	for rows.Next() {
		sig, err := scanSignature(rows)
		if err != nil {
			return model.List[model.Signature]{}, err
		}
		items = append(items, sig)
	}
	return model.List[model.Signature]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

// ListDocuments returns rendered document metadata.
func (r *Reader) ListDocuments(ctx context.Context, f model.ListFilter) (model.List[model.OutputDocument], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if f.Query != "" {
		where = append(where, "contract_id = ?")
		args = append(args, f.Query)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM output_documents WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.OutputDocument]{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+documentColumns+` FROM output_documents WHERE `+clause+
		` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.OutputDocument]{}, err
	}
	defer rows.Close()
	items := []model.OutputDocument{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return model.List[model.OutputDocument]{}, err
		}
		items = append(items, d)
	}
	return model.List[model.OutputDocument]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

// Stats returns dataset counts.
func (r *Reader) Stats(ctx context.Context) (model.Stats, error) {
	var s model.Stats
	for _, q := range []struct {
		sql string
		dst *int
	}{
		{`SELECT COUNT(*) FROM parties`, &s.Parties},
		{`SELECT COUNT(*) FROM contracts`, &s.Contracts},
		{`SELECT COUNT(*) FROM templates`, &s.Templates},
		{`SELECT COUNT(*) FROM signature_assets`, &s.Signatures},
		{`SELECT COUNT(*) FROM output_documents`, &s.Documents},
		{`SELECT COUNT(*) FROM field_definitions WHERE archived = 0`, &s.Fields},
	} {
		if err := r.db.QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
			return s, err
		}
	}
	return s, nil
}

// GetMeta returns a metadata value, or "" when absent.
func (r *Reader) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM app_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SchemaVersion returns the applied migration version.
func (r *Reader) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

// ListTemplateVersions returns every version of a template name.
func (r *Reader) ListTemplateVersions(ctx context.Context, name string) ([]model.Template, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, version, '', variables_json, is_active, created_at, updated_at
		FROM templates WHERE name = ? ORDER BY version DESC`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// VerifyReadOnly asserts the connection rejects writes. Intended for tests.
func (r *Reader) VerifyReadOnly(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS __readonly_probe (x INTEGER)`)
	if err == nil {
		return fmt.Errorf("read-only connection accepted a write")
	}
	return nil
}
