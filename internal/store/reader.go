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
	var idFrontPath, idFrontSHA, idBackPath, idBackSHA, signatureID string
	if err := sc.Scan(&p.ID, &kind, &name, &idNumber, &phone, &address, &note,
		&dataJSON, &idFrontPath, &idFrontSHA, &idBackPath, &idBackSHA,
		&signatureID, &createdAt, &updatedAt); err != nil {
		return p, err
	}
	p.Kind, p.Name, p.IDNumber, p.Phone, p.Address, p.Note = kind, name, idNumber, phone, address, note
	p.Data = parseJSONMap(dataJSON)
	p.IDFrontPath, p.IDFrontSHA256 = idFrontPath, idFrontSHA
	p.IDBackPath, p.IDBackSHA256 = idBackPath, idBackSHA
	p.SignatureID = signatureID
	p.CreatedAt, p.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return p, nil
}

func scanContract(sc scanner) (model.Contract, error) {
	var c model.Contract
	var templateID, partyAID, partyBID, assetID sql.NullString
	var contractNo, title, ctype, status, signedOn, validFrom, validTo string
	var leaseStart, leaseEnd, terminatedOn, terminationReason, terminationNote, dataJSON string
	var sourceContractID, createdAt, updatedAt string
	if err := sc.Scan(&c.ID, &contractNo, &c.Year, &title, &ctype, &templateID,
		&partyAID, &partyBID, &assetID, &status, &signedOn, &validFrom, &validTo,
		&leaseStart, &leaseEnd, &terminatedOn, &terminationReason, &terminationNote,
		&c.Amount, &dataJSON, &sourceContractID, &createdAt, &updatedAt); err != nil {
		return c, err
	}
	c.ContractNo, c.Title, c.Type, c.Status = contractNo, title, ctype, status
	c.TemplateID, c.PartyAID, c.PartyBID = templateID.String, partyAID.String, partyBID.String
	c.AssetID = assetID.String
	c.SignedOn, c.ValidFrom, c.ValidTo = signedOn, validFrom, validTo
	c.LeaseStart, c.LeaseEnd = leaseStart, leaseEnd
	c.TerminatedOn, c.TerminationReason, c.TerminationNote = terminatedOn, terminationReason, terminationNote
	c.ExpiryState = deriveExpiryState(status, leaseEnd, time.Now().UTC())
	c.Data = parseJSONMap(dataJSON)
	c.SourceContractID = sourceContractID
	c.CreatedAt, c.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return c, nil
}

const contractColumns = `id, contract_no, year, title, type, template_id, party_a_id,
	party_b_id, asset_id, status, signed_on, valid_from, valid_to,
	lease_start, lease_end, terminated_on, termination_reason, termination_note,
	amount, data_json, source_contract_id, created_at, updated_at`

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

// sealColumns includes the number of contracts currently linked to the seal.
const sealColumns = `s.id, s.party_id, s.name, s.asset_path, s.sha256, s.mime,
	s.width_px, s.height_px, s.note,
	(SELECT COUNT(*) FROM contract_seals cs WHERE cs.seal_id = s.id),
	s.created_at, s.updated_at`

func scanSeal(sc scanner) (model.Seal, error) {
	var s model.Seal
	var partyID sql.NullString
	var created, updated string
	if err := sc.Scan(&s.ID, &partyID, &s.Name, &s.AssetPath, &s.SHA256, &s.MIME,
		&s.WidthPx, &s.HeightPx, &s.Note, &s.ContractCount, &created, &updated); err != nil {
		return s, err
	}
	s.PartyID = partyID.String
	s.CreatedAt, s.UpdatedAt = parseTime(created), parseTime(updated)
	return s, nil
}

func scanDocument(sc scanner) (model.OutputDocument, error) {
	var d model.OutputDocument
	var contractID, templateID sql.NullString
	var filePath, sha, contentSHA, signatureSnapshot, operator, kind, status, errorMessage string
	var batchID, sourceDocumentID string
	var pageCount int
	var createdAt string
	if err := sc.Scan(&d.ID, &contractID, &templateID, &d.TemplateVersion,
		&filePath, &sha, &pageCount, &contentSHA, &signatureSnapshot,
		&operator, &kind, &status, &errorMessage, &batchID, &sourceDocumentID,
		&createdAt); err != nil {
		return d, err
	}
	d.ContractID, d.TemplateID = contractID.String, templateID.String
	d.FilePath, d.SHA256, d.PageCount = filePath, sha, pageCount
	d.ContentSHA256 = contentSHA
	d.Signatures = parseDocumentSignatures(signatureSnapshot)
	d.Operator, d.Kind, d.Status = operator, kind, status
	d.ErrorMessage, d.BatchID, d.SourceDocumentID = errorMessage, batchID, sourceDocumentID
	d.CreatedAt = parseTime(createdAt)
	return d, nil
}

func parseDocumentSignatures(raw string) []model.DocumentSignatureSnapshot {
	out := []model.DocumentSignatureSnapshot{}
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []model.DocumentSignatureSnapshot{}
	}
	return out
}

const documentColumns = `id, contract_id, template_id, template_version,
	file_path, sha256, page_count, content_sha256, signature_snapshot,
	operator, kind, status, error_message, batch_id, source_document_id, created_at`

// GetDocument returns one generated document record by id.
func (r *Reader) GetDocument(ctx context.Context, id string) (model.OutputDocument, error) {
	d, err := scanDocument(r.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM output_documents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.OutputDocument{}, ErrNotFound
	}
	return d, err
}

// LatestSuccessfulDocument returns the newest successful PDF of a contract.
// Regenerate flows use it as the default source document.
//
// created_at has second precision, so several documents can share a timestamp;
// rowid gives strict insertion order. Ordering by id would pick a random row
// within the same second and make regenerate point at an older document.
func (r *Reader) LatestSuccessfulDocument(ctx context.Context, contractID string) (model.OutputDocument, error) {
	d, err := scanDocument(r.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM output_documents
		 WHERE contract_id = ? AND status = ? ORDER BY rowid DESC LIMIT 1`,
		contractID, model.DocumentStatusSuccess))
	if errors.Is(err, sql.ErrNoRows) {
		return model.OutputDocument{}, ErrNotFound
	}
	return d, err
}

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
	data_json, id_front_path, id_front_sha256, id_back_path, id_back_sha256,
	signature_asset_id, created_at, updated_at`

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
	if partyID := strings.TrimSpace(f.PartyID); partyID != "" {
		where = append(where, "(party_a_id = ? OR party_b_id = ?)")
		args = append(args, partyID, partyID)
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

// FindPartiesByIDNumber returns parties whose trimmed ID number equals
// idNumber. An empty idNumber matches nothing: blank IDs are not duplicates of
// each other (old archives are allowed to stay unbackfilled).
func (r *Reader) FindPartiesByIDNumber(ctx context.Context, idNumber string) ([]model.Party, error) {
	idNumber = strings.TrimSpace(idNumber)
	if idNumber == "" {
		return []model.Party{}, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+partyColumns+` FROM parties WHERE trim(id_number) = ? ORDER BY created_at, id`, idNumber)
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
	detail := model.ContractDetail{
		Contract:   c,
		Signatures: []model.Signature{},
		Seals:      []model.Seal{},
		Documents:  []model.OutputDocument{},
	}
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
	if c.AssetID != "" {
		if a, err := r.GetAsset(ctx, c.AssetID); err == nil {
			detail.Asset = &a
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
	seals, err := r.ListSealsForContract(ctx, c.ID)
	if err != nil {
		return detail, err
	}
	detail.Seals = seals
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
		 ORDER BY created_at DESC, rowid DESC`, contractID)
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

// templateColumns selects template metadata without the HTML body. The empty
// literal keeps the scan order identical to GetTemplate, which includes html.
const templateColumns = `id, name, version, kind, '', variables_json, is_active,
	source_path, pdf_path, mapping_json, page_count, converter,
	source_sha256, pdf_sha256, mask_field_values, created_at, updated_at`

const templateColumnsFull = `id, name, version, kind, html, variables_json, is_active,
	source_path, pdf_path, mapping_json, page_count, converter,
	source_sha256, pdf_sha256, mask_field_values, created_at, updated_at`

// ListTemplates returns template metadata (HTML omitted).
func (r *Reader) ListTemplates(ctx context.Context, f model.ListFilter) (model.List[model.Template], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+q+"%")
	}
	if kind := strings.TrimSpace(f.Kind); kind != "" {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM templates WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Template]{}, err
	}
	query := `SELECT ` + templateColumns + ` FROM templates WHERE ` + clause +
		` ORDER BY name, version DESC LIMIT ? OFFSET ?`
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
	var kind, variablesJSON, sourcePath, pdfPath, mappingJSON, converter string
	var sourceSHA, pdfSHA, createdAt, updatedAt string
	var active, maskFieldValues int
	if err := sc.Scan(&t.ID, &t.Name, &t.Version, &kind, &t.HTML, &variablesJSON, &active,
		&sourcePath, &pdfPath, &mappingJSON, &t.PageCount, &converter,
		&sourceSHA, &pdfSHA, &maskFieldValues, &createdAt, &updatedAt); err != nil {
		return t, err
	}
	t.Kind, t.SourcePath, t.PDFPath = kind, sourcePath, pdfPath
	t.Converter, t.SourceSHA256, t.PDFSHA256 = converter, sourceSHA, pdfSHA
	t.Variables = parseJSONStrings(variablesJSON)
	t.Placements = parsePlacements(mappingJSON)
	t.IsActive = active != 0
	t.MaskFieldValues = maskFieldValues != 0
	t.CreatedAt, t.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return t, nil
}

func parsePlacements(raw string) []model.Placement {
	if raw == "" {
		return []model.Placement{}
	}
	var mapping templateMapping
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return []model.Placement{}
	}
	if mapping.Placements == nil {
		return []model.Placement{}
	}
	return mapping.Placements
}

// GetTemplate returns one template including its HTML and placements.
func (r *Reader) GetTemplate(ctx context.Context, id string) (model.Template, error) {
	t, err := scanTemplate(r.db.QueryRowContext(ctx, `SELECT `+templateColumnsFull+
		` FROM templates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Template{}, ErrNotFound
	}
	return t, err
}

// GetSignature returns one signature metadata row.
func (r *Reader) GetSignature(ctx context.Context, id string) (model.Signature, error) {
	sig, err := scanSignature(r.db.QueryRowContext(ctx,
		`SELECT `+signatureColumns+` FROM signature_assets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Signature{}, ErrNotFound
	}
	return sig, err
}

// ReferencedAssetPaths returns every asset path recorded by the database.
// B9 uses it to report orphan files and missing files without deleting
// anything. Unknown future asset tables are intentionally not guessed.
func (r *Reader) ReferencedAssetPaths(ctx context.Context) ([]string, error) {
	queries := []string{
		`SELECT asset_path FROM signature_assets`,
		`SELECT asset_path FROM seal_assets`,
		`SELECT source_path FROM templates WHERE source_path <> ''`,
		`SELECT pdf_path FROM templates WHERE pdf_path <> ''`,
		`SELECT file_path FROM output_documents`,
	}
	out := []string{}
	for _, query := range queries {
		rows, err := r.db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return nil, err
			}
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
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

// ListSeals returns seal asset metadata.
func (r *Reader) ListSeals(ctx context.Context, f model.ListFilter) (model.List[model.Seal], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(s.name LIKE ? OR s.note LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if partyID := strings.TrimSpace(f.PartyID); partyID != "" {
		where = append(where, "s.party_id = ?")
		args = append(args, partyID)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seal_assets s WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.Seal]{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+sealColumns+` FROM seal_assets s WHERE `+clause+
		` ORDER BY s.name, s.created_at DESC, s.id LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return model.List[model.Seal]{}, err
	}
	defer rows.Close()
	items := []model.Seal{}
	for rows.Next() {
		seal, err := scanSeal(rows)
		if err != nil {
			return model.List[model.Seal]{}, err
		}
		items = append(items, seal)
	}
	return model.List[model.Seal]{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, rows.Err()
}

// GetSeal returns one seal by id.
func (r *Reader) GetSeal(ctx context.Context, id string) (model.Seal, error) {
	seal, err := scanSeal(r.db.QueryRowContext(ctx, `SELECT `+sealColumns+
		` FROM seal_assets s WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Seal{}, ErrNotFound
	}
	return seal, err
}

// ListSealsForContract returns seals explicitly linked to one contract,
// most recently placed first.
func (r *Reader) ListSealsForContract(ctx context.Context, contractID string) ([]model.Seal, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sealColumns+`
		FROM seal_assets s JOIN contract_seals cs ON cs.seal_id = s.id
		WHERE cs.contract_id = ?
		ORDER BY cs.placed_at DESC, s.name, s.id`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSeals(rows)
}

// ListSealsForParty returns seals that belong to one party.
func (r *Reader) ListSealsForParty(ctx context.Context, partyID string) ([]model.Seal, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sealColumns+`
		FROM seal_assets s WHERE s.party_id = ?
		ORDER BY s.created_at DESC, s.id`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSeals(rows)
}

func scanSeals(rows *sql.Rows) ([]model.Seal, error) {
	out := []model.Seal{}
	for rows.Next() {
		seal, err := scanSeal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, seal)
	}
	return out, rows.Err()
}

// SealContractIDs returns the contracts a seal is linked to.
func (r *Reader) SealContractIDs(ctx context.Context, sealID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT contract_id FROM contract_seals WHERE seal_id = ? ORDER BY placed_at, contract_id`, sealID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListDocuments returns rendered document metadata. It can be filtered by
// contract (contract_id), template version (template_id), batch (batch_id) and
// status (success/failed) — the B4 generation-history query surface.
func (r *Reader) ListDocuments(ctx context.Context, f model.ListFilter) (model.List[model.OutputDocument], error) {
	f.Normalize()
	where := []string{"1=1"}
	args := []any{}
	contractID := strings.TrimSpace(f.ContractID)
	if contractID == "" {
		contractID = strings.TrimSpace(f.Query)
	}
	if contractID != "" {
		where = append(where, "contract_id = ?")
		args = append(args, contractID)
	}
	if templateID := strings.TrimSpace(f.TemplateID); templateID != "" {
		where = append(where, "template_id = ?")
		args = append(args, templateID)
	}
	if batchID := strings.TrimSpace(f.BatchID); batchID != "" {
		where = append(where, "batch_id = ?")
		args = append(args, batchID)
	}
	switch status := strings.TrimSpace(f.Status); status {
	case model.DocumentStatusSuccess, model.DocumentStatusFailed:
		where = append(where, "status = ?")
		args = append(args, status)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM output_documents WHERE `+clause, args...).Scan(&total); err != nil {
		return model.List[model.OutputDocument]{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+documentColumns+` FROM output_documents WHERE `+clause+
		` ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
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
		{`SELECT COUNT(*) FROM seal_assets`, &s.Seals},
		{`SELECT COUNT(*) FROM output_documents WHERE status = 'success'`, &s.Documents},
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
	rows, err := r.db.QueryContext(ctx, `SELECT `+templateColumns+
		` FROM templates WHERE name = ? ORDER BY version DESC`, name)
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

// MaxTemplateVersion returns the highest version number used by a template
// family (both HTML and PDF kinds share the (name, version) uniqueness rule).
func (r *Reader) MaxTemplateVersion(ctx context.Context, name string) (int, error) {
	var v int
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM templates WHERE name = ?`, name).Scan(&v)
	return v, err
}

// TemplateUsage summarizes how widely a template family is referenced. It
// backs the change-impact warning shown before a new version is saved.
func (r *Reader) TemplateUsage(ctx context.Context, id string) (model.TemplateUsage, error) {
	tpl, err := r.GetTemplate(ctx, id)
	if err != nil {
		return model.TemplateUsage{}, err
	}
	usage := model.TemplateUsage{
		TemplateID: tpl.ID, Name: tpl.Name, Version: tpl.Version,
	}
	for _, q := range []struct {
		sql  string
		args []any
		dst  *int
	}{
		{`SELECT COUNT(*) FROM contracts WHERE template_id = ?`, []any{tpl.ID}, &usage.VersionContracts},
		{`SELECT COUNT(*) FROM contracts c JOIN templates t ON c.template_id = t.id
			WHERE t.name = ? AND t.kind = ?`, []any{tpl.Name, tpl.Kind}, &usage.FamilyContracts},
		{`SELECT COUNT(*) FROM output_documents d JOIN templates t ON d.template_id = t.id
			WHERE t.name = ? AND t.kind = ? AND d.status = 'success'`, []any{tpl.Name, tpl.Kind}, &usage.Documents},
		{`SELECT COUNT(*) FROM templates WHERE name = ? AND kind = ?`, []any{tpl.Name, tpl.Kind}, &usage.FamilyVersions},
		{`SELECT COALESCE(MAX(version), 0) FROM templates WHERE name = ? AND kind = ?`, []any{tpl.Name, tpl.Kind}, &usage.LatestVersion},
	} {
		if err := r.db.QueryRowContext(ctx, q.sql, q.args...).Scan(q.dst); err != nil {
			return usage, err
		}
	}
	usage.OtherVersionContracts = usage.FamilyContracts - usage.VersionContracts
	if usage.OtherVersionContracts < 0 {
		usage.OtherVersionContracts = 0
	}
	return usage, nil
}

// TemplateReferenceCounts returns how many contracts and output documents
// point at one exact template version. Deleting a referenced version would
// detach those records, so the service refuses it.
func (r *Reader) TemplateReferenceCounts(ctx context.Context, id string) (contracts, documents int, err error) {
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM contracts WHERE template_id = ?`, id).Scan(&contracts); err != nil {
		return 0, 0, err
	}
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM output_documents WHERE template_id = ? AND status = 'success'`, id).Scan(&documents); err != nil {
		return 0, 0, err
	}
	return contracts, documents, nil
}

// VerifyReadOnly asserts the connection rejects writes. Intended for tests.
func (r *Reader) VerifyReadOnly(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS __readonly_probe (x INTEGER)`)
	if err == nil {
		return fmt.Errorf("read-only connection accepted a write")
	}
	return nil
}
