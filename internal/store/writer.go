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
		INSERT INTO parties (id, kind, name, id_number, phone, address, note, data_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			kind = excluded.kind, name = excluded.name, id_number = excluded.id_number,
			phone = excluded.phone, address = excluded.address, note = excluded.note,
			data_json = excluded.data_json, updated_at = excluded.updated_at`,
		p.ID, p.Kind, p.Name, p.IDNumber, p.Phone, p.Address, p.Note, string(data),
		formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
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
	case model.ContractDraft, model.ContractActive, model.ContractArchived:
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
			party_b_id, status, signed_on, valid_from, valid_to, amount, data_json,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			contract_no = excluded.contract_no, year = excluded.year,
			title = excluded.title, type = excluded.type,
			template_id = excluded.template_id,
			party_a_id = excluded.party_a_id, party_b_id = excluded.party_b_id,
			status = excluded.status, signed_on = excluded.signed_on,
			valid_from = excluded.valid_from, valid_to = excluded.valid_to,
			amount = excluded.amount,
			data_json = excluded.data_json, updated_at = excluded.updated_at`,
		c.ID, c.ContractNo, c.Year, c.Title, c.Type, nullString(c.TemplateID),
		nullString(c.PartyAID), nullString(c.PartyBID), c.Status,
		c.SignedOn, c.ValidFrom, c.ValidTo, c.Amount, string(data),
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

// PutTemplate inserts or updates a template.
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
	if t.HTML == "" {
		return fmt.Errorf("template html is required")
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
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO templates (id, name, version, html, variables_json, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name, version = excluded.version, html = excluded.html,
			variables_json = excluded.variables_json, is_active = excluded.is_active,
			updated_at = excluded.updated_at`,
		t.ID, t.Name, t.Version, t.HTML, string(vars), boolInt(t.IsActive),
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put template: %w", err)
	}
	return nil
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

// PutOutputDocument inserts or updates a rendered document record.
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
	if d.FilePath == "" || d.SHA256 == "" {
		return fmt.Errorf("document file_path and sha256 are required")
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = model.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO output_documents (id, contract_id, template_id, template_version,
			file_path, sha256, page_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			contract_id = excluded.contract_id, template_id = excluded.template_id,
			template_version = excluded.template_version, file_path = excluded.file_path,
			sha256 = excluded.sha256, page_count = excluded.page_count`,
		d.ID, nullString(d.ContractID), nullString(d.TemplateID), d.TemplateVersion,
		d.FilePath, d.SHA256, d.PageCount, formatTime(d.CreatedAt))
	if err != nil {
		return fmt.Errorf("put output document: %w", err)
	}
	return nil
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
