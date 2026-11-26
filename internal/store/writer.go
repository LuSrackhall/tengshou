package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// PutParty inserts or updates a party.
func (s *Store) PutParty(ctx context.Context, p *model.Party) error {
	if p == nil {
		return fmt.Errorf("party is nil")
	}
	if p.ID == "" {
		id, err := model.NewID("pty")
		if err != nil {
			return err
		}
		p.ID = id
	}
	if p.Kind == "" {
		p.Kind = model.PartyPerson
	}
	if p.Kind != model.PartyPerson && p.Kind != model.PartyOrganization {
		return fmt.Errorf("invalid party kind %q", p.Kind)
	}
	if p.Name == "" {
		return fmt.Errorf("party name is required")
	}
	now := model.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	data, err := json.Marshal(nonNil(p.Data))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO parties (id, kind, name, id_number, phone, address, note, data_json,
			id_front_path, id_front_sha256, id_back_path, id_back_sha256,
			signature_asset_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			kind = excluded.kind, name = excluded.name, id_number = excluded.id_number,
			phone = excluded.phone, address = excluded.address, note = excluded.note,
			data_json = excluded.data_json,
			id_front_path = excluded.id_front_path,
			id_front_sha256 = excluded.id_front_sha256,
			id_back_path = excluded.id_back_path,
			id_back_sha256 = excluded.id_back_sha256,
			signature_asset_id = excluded.signature_asset_id,
			updated_at = excluded.updated_at`,
		p.ID, p.Kind, p.Name, p.IDNumber, p.Phone, p.Address, p.Note, string(data),
		p.IDFrontPath, p.IDFrontSHA256, p.IDBackPath, p.IDBackSHA256,
		p.SignatureID, formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put party: %w", err)
	}
	return nil
}

// PutContract inserts or updates a contract.
func (s *Store) PutContract(ctx context.Context, c *model.Contract) error {
	if c == nil {
		return fmt.Errorf("contract is nil")
	}
	if c.ID == "" {
		id, err := model.NewID("ctr")
		if err != nil {
			return err
		}
		c.ID = id
	}
	if c.Title == "" {
		return fmt.Errorf("contract title is required")
	}
	if c.Status == "" {
		c.Status = model.ContractDraft
	}
	switch c.Status {
	case model.ContractDraft, model.ContractActive, model.ContractTerminated, model.ContractArchived:
	default:
		return fmt.Errorf("invalid contract status %q", c.Status)
	}
	if c.ContractNo == "" {
		// Direct store callers bypass the service numbering; keep a stable,
		// unique fallback so the uniqueness index never sees an empty number
		// on a persisted row.
		c.ContractNo = "C-" + c.ID
	}
	if c.Year == 0 {
		c.Year = guessYear(c.SignedOn, model.Now())
	}
	now := model.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	data, err := json.Marshal(nonNil(c.Data))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO contracts (id, contract_no, year, title, type, template_id, party_a_id,
			party_b_id, asset_id, status, signed_on, valid_from, valid_to,
			lease_start, lease_end, terminated_on, termination_reason, termination_note,
			amount, data_json, source_contract_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			contract_no = excluded.contract_no, year = excluded.year,
			title = excluded.title, type = excluded.type,
			template_id = excluded.template_id,
			party_a_id = excluded.party_a_id, party_b_id = excluded.party_b_id,
			asset_id = excluded.asset_id,
			status = excluded.status, signed_on = excluded.signed_on,
			valid_from = excluded.valid_from, valid_to = excluded.valid_to,
			lease_start = excluded.lease_start, lease_end = excluded.lease_end,
			terminated_on = excluded.terminated_on,
			termination_reason = excluded.termination_reason,
			termination_note = excluded.termination_note,
			amount = excluded.amount,
			data_json = excluded.data_json, updated_at = excluded.updated_at`,
		c.ID, c.ContractNo, c.Year, c.Title, c.Type, nullString(c.TemplateID),
		nullString(c.PartyAID), nullString(c.PartyBID), nullString(c.AssetID),
		c.Status, c.SignedOn, c.ValidFrom, c.ValidTo, c.LeaseStart, c.LeaseEnd,
		c.TerminatedOn, c.TerminationReason, c.TerminationNote,
		c.Amount, string(data), c.SourceContractID,
		formatTime(c.CreatedAt), formatTime(c.UpdatedAt))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("%w: %s", ErrDuplicateContractNo, c.ContractNo)
		}
		return fmt.Errorf("put contract: %w", err)
	}
	return nil
}

func guessYear(date string, now time.Time) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil && y >= 1900 && y <= 2200 {
			return y
		}
	}
	return now.Year()
}

// PutTemplate inserts or updates a template version.
func (s *Store) PutTemplate(ctx context.Context, t *model.Template) error {
	if t == nil {
		return fmt.Errorf("template is nil")
	}
	if t.ID == "" {
		id, err := model.NewID("tpl")
		if err != nil {
			return err
		}
		t.ID = id
	}
	if t.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if t.Version <= 0 {
		t.Version = 1
	}
	if t.Kind == "" {
		t.Kind = model.TemplateKindHTML
	}
	switch t.Kind {
	case model.TemplateKindHTML:
		if t.HTML == "" {
			return fmt.Errorf("template html is required")
		}
	case model.TemplateKindPDF:
		if t.PDFPath == "" {
			return fmt.Errorf("template pdf path is required")
		}
		if t.PageCount <= 0 {
			return fmt.Errorf("template page count is required")
		}
	default:
		return fmt.Errorf("invalid template kind %q", t.Kind)
	}
	now := model.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	vars, err := json.Marshal(nonNilStrings(t.Variables))
	if err != nil {
		return err
	}
	mapping, err := json.Marshal(templateMapping{
		Version:    1,
		Placements: nonNilPlacements(t.Placements),
	})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO templates (id, name, version, kind, html, variables_json, is_active,
			source_path, pdf_path, mapping_json, page_count, converter,
			source_sha256, pdf_sha256, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name, version = excluded.version, kind = excluded.kind,
			html = excluded.html,
			variables_json = excluded.variables_json, is_active = excluded.is_active,
			source_path = excluded.source_path, pdf_path = excluded.pdf_path,
			mapping_json = excluded.mapping_json, page_count = excluded.page_count,
			converter = excluded.converter, source_sha256 = excluded.source_sha256,
			pdf_sha256 = excluded.pdf_sha256, updated_at = excluded.updated_at`,
		t.ID, t.Name, t.Version, t.Kind, t.HTML, string(vars), boolInt(t.IsActive),
		t.SourcePath, t.PDFPath, string(mapping), t.PageCount, t.Converter,
		t.SourceSHA256, t.PDFSHA256, formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("%w: %s v%d", ErrDuplicateTemplateVersion, t.Name, t.Version)
		}
		return fmt.Errorf("put template: %w", err)
	}
	return nil
}

// DeleteTemplate removes one template version. Contracts and output documents
// referencing it have their template_id set to NULL by the schema, so callers
// must check references first when that would lose information.
func (s *Store) DeleteTemplate(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeactivateOtherTemplateVersions marks every other version of a template
// family inactive. It is called after a new version is persisted so the UI can
// treat is_active as "the current version".
func (s *Store) DeactivateOtherTemplateVersions(ctx context.Context, name, exceptID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE templates SET is_active = 0 WHERE name = ? AND id <> ?`, name, exceptID)
	if err != nil {
		return fmt.Errorf("deactivate template versions: %w", err)
	}
	return nil
}

// templateMapping is the persisted shape of templates.mapping_json. The
// version field leaves room for future changes to the coordinate semantics.
type templateMapping struct {
	Version    int               `json:"version"`
	Placements []model.Placement `json:"placements"`
}

func nonNilPlacements(v []model.Placement) []model.Placement {
	if v == nil {
		return []model.Placement{}
	}
	return v
}

// PutSignature inserts or updates a signature asset record.
func (s *Store) PutSignature(ctx context.Context, sig *model.Signature) error {
	if sig == nil {
		return fmt.Errorf("signature is nil")
	}
	if sig.ID == "" {
		id, err := model.NewID("sig")
		if err != nil {
			return err
		}
		sig.ID = id
	}
	if sig.AssetPath == "" || sig.SHA256 == "" {
		return fmt.Errorf("signature asset_path and sha256 are required")
	}
	if sig.CreatedAt.IsZero() {
		sig.CreatedAt = model.Now()
	}
	if sig.CapturedAt.IsZero() {
		sig.CapturedAt = sig.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO signature_assets (id, party_id, contract_id, signer_name, asset_path,
			sha256, content_sha256, source, session_id, width_px, height_px,
			captured_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			party_id = excluded.party_id, contract_id = excluded.contract_id,
			signer_name = excluded.signer_name,
			asset_path = excluded.asset_path, sha256 = excluded.sha256,
			content_sha256 = excluded.content_sha256, source = excluded.source,
			session_id = excluded.session_id,
			width_px = excluded.width_px, height_px = excluded.height_px,
			captured_at = excluded.captured_at`,
		sig.ID, nullString(sig.PartyID), nullString(sig.ContractID), sig.SignerName,
		sig.AssetPath, sig.SHA256, sig.ContentSHA256, sig.Source, sig.SessionID,
		sig.WidthPx, sig.HeightPx, formatTime(sig.CapturedAt), formatTime(sig.CreatedAt))
	if err != nil {
		return fmt.Errorf("put signature: %w", err)
	}
	return nil
}

// PutOutputDocument inserts or updates a rendered document record. Successful
// rows must point at a file; failed attempts keep only the error message.
func (s *Store) PutOutputDocument(ctx context.Context, d *model.OutputDocument) error {
	if d == nil {
		return fmt.Errorf("document is nil")
	}
	if d.ID == "" {
		id, err := model.NewID("doc")
		if err != nil {
			return err
		}
		d.ID = id
	}
	if d.Status == "" {
		d.Status = model.DocumentStatusSuccess
	}
	if d.Kind == "" {
		d.Kind = model.DocumentKindInitial
	}
	switch d.Status {
	case model.DocumentStatusSuccess:
		if d.FilePath == "" || d.SHA256 == "" {
			return fmt.Errorf("document file_path and sha256 are required")
		}
	case model.DocumentStatusFailed:
	default:
		return fmt.Errorf("invalid document status %q", d.Status)
	}
	if d.Signatures == nil {
		d.Signatures = []model.DocumentSignatureSnapshot{}
	}
	signatureJSON, err := json.Marshal(d.Signatures)
	if err != nil {
		return fmt.Errorf("encode signature snapshot: %w", err)
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = model.Now()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO output_documents (id, contract_id, template_id, template_version,
			file_path, sha256, page_count, content_sha256, signature_snapshot,
			operator, kind, status, error_message, batch_id, source_document_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			contract_id = excluded.contract_id, template_id = excluded.template_id,
			template_version = excluded.template_version, file_path = excluded.file_path,
			sha256 = excluded.sha256, page_count = excluded.page_count,
			content_sha256 = excluded.content_sha256,
			signature_snapshot = excluded.signature_snapshot,
			operator = excluded.operator, kind = excluded.kind, status = excluded.status,
			error_message = excluded.error_message, batch_id = excluded.batch_id,
			source_document_id = excluded.source_document_id`,
		d.ID, nullString(d.ContractID), nullString(d.TemplateID), d.TemplateVersion,
		d.FilePath, d.SHA256, d.PageCount, d.ContentSHA256, string(signatureJSON),
		d.Operator, d.Kind, d.Status, d.ErrorMessage, d.BatchID, d.SourceDocumentID,
		formatTime(d.CreatedAt))
	if err != nil {
		return fmt.Errorf("put output document: %w", err)
	}
	return nil
}

// PutSeal inserts or updates a seal asset record.
func (s *Store) PutSeal(ctx context.Context, seal *model.Seal) error {
	if seal == nil {
		return fmt.Errorf("seal is nil")
	}
	if seal.ID == "" {
		id, err := model.NewID("seal")
		if err != nil {
			return err
		}
		seal.ID = id
	}
	if strings.TrimSpace(seal.Name) == "" {
		return fmt.Errorf("seal name is required")
	}
	if seal.AssetPath == "" || seal.SHA256 == "" {
		return fmt.Errorf("seal asset_path and sha256 are required")
	}
	if seal.MIME == "" {
		seal.MIME = "image/png"
	}
	now := model.Now()
	if seal.CreatedAt.IsZero() {
		seal.CreatedAt = now
	}
	seal.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO seal_assets (id, party_id, name, asset_path, sha256, mime,
			width_px, height_px, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			party_id = excluded.party_id, name = excluded.name,
			asset_path = excluded.asset_path, sha256 = excluded.sha256,
			mime = excluded.mime, width_px = excluded.width_px,
			height_px = excluded.height_px, note = excluded.note,
			updated_at = excluded.updated_at`,
		seal.ID, nullString(seal.PartyID), seal.Name, seal.AssetPath, seal.SHA256,
		seal.MIME, seal.WidthPx, seal.HeightPx, seal.Note,
		formatTime(seal.CreatedAt), formatTime(seal.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put seal: %w", err)
	}
	return nil
}

// DeleteSeal removes a seal record. Contract links cascade; the asset file is
// removed by the service layer after the database change commits.
func (s *Store) DeleteSeal(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM contract_seals WHERE seal_id = ?`, id); err != nil {
		return fmt.Errorf("unlink seal: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM seal_assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete seal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// AttachSeal links one seal to contracts, ignoring links that already exist.
// It returns how many new links were created.
func (s *Store) AttachSeal(ctx context.Context, sealID string, contractIDs []string, placedAt time.Time) (int, error) {
	if len(contractIDs) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	attached := 0
	for _, contractID := range contractIDs {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO contract_seals (contract_id, seal_id, placed_at)
			VALUES (?, ?, ?)
			ON CONFLICT (contract_id, seal_id) DO NOTHING`,
			contractID, sealID, formatTime(placedAt))
		if err != nil {
			return 0, fmt.Errorf("attach seal: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		attached += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return attached, nil
}

// DetachSeal removes seal links from contracts and reports how many existed.
func (s *Store) DetachSeal(ctx context.Context, sealID string, contractIDs []string) (int, error) {
	if len(contractIDs) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	removed := 0
	for _, contractID := range contractIDs {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM contract_seals WHERE seal_id = ? AND contract_id = ?`, sealID, contractID)
		if err != nil {
			return 0, fmt.Errorf("detach seal: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		removed += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return removed, nil
}

// SetMeta stores an application metadata key.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
