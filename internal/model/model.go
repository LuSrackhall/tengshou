// Package model defines the stable structured data exposed by 腾手.
//
// The JSON field names in this package are part of the read-only data API
// (consumed by the B6 MCP connector and user-side agents). Keep them stable;
// add fields instead of renaming them.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Entity kinds.
const (
	PartyPerson       = "person"
	PartyOrganization = "organization"
)

// Asset kinds. v0.2 only supports houses; the discriminator lets later
// versions add other rental objects (parking spots, equipment) with their own
// fields and validation without touching contracts or payments.
const AssetTypeHouse = "house"

// Asset lease statuses are derived from contracts and never stored: an asset
// is rented while it has an effective, non-terminated contract, and vacant
// otherwise. Contract expiry alone does not release an asset (te-l0hh0 D4).
const (
	LeaseStatusRented = "rented"
	LeaseStatusVacant = "vacant"
)

// Contract statuses. A terminated contract stays in the ledger: termination is
// an explicit act recorded with a date and a reason, while lease expiry alone
// never changes the status or releases the asset (te-l0hh0 D4).
const (
	ContractDraft      = "draft"
	ContractActive     = "active"
	ContractTerminated = "terminated"
	ContractArchived   = "archived"
)

// Termination reasons. Renewal is set by the server when a renewal contract
// takes effect; the other values are chosen by the user.
const (
	TerminationReasonEarly   = "early"   // 提前退租
	TerminationReasonVoid    = "void"    // 作废
	TerminationReasonRenewal = "renewal" // 续签联动
	TerminationReasonOther   = "other"   // 其他
)

// Lease expiry reminder states are derived at read time for active contracts
// only. They are labels, never stored and never trigger a status change.
const (
	ExpiryStateExpiring = "expiring" // 30 天内到期
	ExpiryStateExpired  = "expired"  // 已到期未续签
	// ExpiryReminderDays is the "about to expire" window used by the ledger,
	// the home stats and derived expiry_state.
	ExpiryReminderDays = 30
)

// Field definition entities. Field definitions make the data model
// configurable at runtime: users add their own fields without a release.
const (
	FieldEntityContract = "contract"
	FieldEntityParty    = "party"
)

// Field types. They describe how values are validated, edited and exported.
const (
	FieldTypeText     = "text"
	FieldTypeLongText = "longtext"
	FieldTypeNumber   = "number"
	FieldTypeMoney    = "money"
	FieldTypeDate     = "date"
	FieldTypeSelect   = "select"
	FieldTypeBool     = "bool"
	FieldTypeImage    = "image"
	// Built-in only: the counterparty (resolved to a party record) and the
	// contract status (mapped to draft/active/archived).
	FieldTypeParty  = "party"
	FieldTypeStatus = "status"
)

// Built-in field keys. They map to first-class columns; user-defined fields
// live in the data JSON object under their key.
const (
	FieldContractNo = "contract_no"
	FieldYear       = "year"
	FieldTitle      = "title"
	FieldType       = "type"
	FieldPartyB     = "party_b"
	FieldAmount     = "amount"
	FieldSignedOn   = "signed_on"
	FieldValidFrom  = "valid_from"
	FieldValidTo    = "valid_to"
	// FieldLeaseStart / FieldLeaseEnd are the formal lease period (v0.2). They
	// are distinct from valid_from/valid_to, which describe when the contract
	// itself takes effect: the lease period drives asset occupancy.
	FieldLeaseStart = "lease_start"
	FieldLeaseEnd   = "lease_end"
	FieldStatus     = "status"
	// FieldAssetID binds a contract to one property archive. It is not a
	// configurable field: the form uses a dedicated asset picker.
	FieldAssetID    = "asset_id"
	FieldPartyName  = "name"
	FieldPartyKind  = "kind"
	FieldPartyIDNo  = "id_number"
	FieldPartyPhone = "phone"
	FieldPartyAddr  = "address"
	FieldPartyNote  = "note"
)

// Change-log entities/actions.
const (
	ChangeEntityContract  = "contract"
	ChangeEntityParty     = "party"
	ChangeEntityAsset     = "asset"
	ChangeEntityField     = "field"
	ChangeEntitySeal      = "seal"
	ChangeEntityTemplate  = "template"
	ChangeEntitySignature = "signature"
	ChangeEntityBackup    = "backup"
	ChangeEntitySettings  = "settings"
	// ChangeEntityImport records import-level (file) audit entries; per-row
	// changes stay attached to their contract entries.
	ChangeEntityImport = "import"
	// ChangeEntityMigration records one-time legacy data migrations (for
	// example turning old contract address fields into property archives);
	// the entry carries a run-level summary rather than per-row diffs.
	ChangeEntityMigration = "migration"
	ChangeActionCreate    = "create"
	ChangeActionUpdate    = "update"
	ChangeActionDelete    = "delete"
	ChangeActionArchive   = "archive"
	ChangeActionImport    = "import"
	ChangeActionExport    = "export"
	ChangeActionRestore   = "restore"
	ChangeActionGenerate  = "generate"
)

// Party ID-photo sides for the archive upgrade (v0.2).
const (
	PartyPhotoFront = "front"
	PartyPhotoBack  = "back"
)

// Party is a counterparty (person or organization) in a contract. The v0.2
// archive upgrade adds the ID-card photos and an archive-level handwritten
// signature that can be reused by later contracts.
type Party struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	IDNumber string         `json:"id_number,omitempty"`
	Phone    string         `json:"phone,omitempty"`
	Address  string         `json:"address,omitempty"`
	Note     string         `json:"note,omitempty"`
	Data     map[string]any `json:"data"`
	// IDFrontPath / IDBackPath are asset paths (relative to the data dir) of
	// the compressed ID-card photos; the digest pins the exact file.
	IDFrontPath   string `json:"id_front_path,omitempty"`
	IDFrontSHA256 string `json:"id_front_sha256,omitempty"`
	IDBackPath    string `json:"id_back_path,omitempty"`
	IDBackSHA256  string `json:"id_back_sha256,omitempty"`
	// SignatureID points at the archive-level retained signature asset. It is
	// captured once and can be reused by later contracts as-is (v0.2 D3=B).
	SignatureID string    `json:"signature_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Asset is one rentable property record (v0.2: houses only). The minimal
// fields follow te-l0hh0 D1: name, address, room number, area and note.
// Contracts and payments will reference only the asset id and lease period,
// never house-specific fields. LeaseStatus is derived at read time from the
// contracts that reference the asset; it is not persisted, so a stopped or
// expired contract can never leave a stale "rented" flag behind.
type Asset struct {
	ID        string   `json:"id"`
	AssetType string   `json:"asset_type"`
	Name      string   `json:"name"`
	Address   string   `json:"address,omitempty"`
	RoomNo    string   `json:"room_no,omitempty"`
	AreaSqm   *float64 `json:"area_sqm,omitempty"`
	Note      string   `json:"note,omitempty"`
	Enabled   bool     `json:"enabled"`
	// LeaseStatus is rented or vacant. It is derived from contracts; the
	// contract→asset link lands in a follow-up bead, so until then every asset
	// is vacant (no contract can reference it yet).
	LeaseStatus string    `json:"lease_status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Contract is one agreement between two parties. v0.2 makes the third party
// explicit: AssetID points at the rented property archive, and the lease
// period is kept separately from the contract's own validity dates.
type Contract struct {
	ID         string `json:"id"`
	ContractNo string `json:"contract_no"`
	Year       int    `json:"year"`
	Title      string `json:"title"`
	Type       string `json:"type,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	PartyAID   string `json:"party_a_id,omitempty"`
	PartyBID   string `json:"party_b_id,omitempty"`
	// AssetID is empty for historical contracts created before the asset
	// archive existed; those rows keep displaying and editing normally.
	AssetID   string `json:"asset_id,omitempty"`
	Status    string `json:"status"`
	SignedOn  string `json:"signed_on,omitempty"`
	ValidFrom string `json:"valid_from,omitempty"`
	ValidTo   string `json:"valid_to,omitempty"`
	// LeaseStart / LeaseEnd are the rental period (YYYY-MM-DD). Empty on
	// historical rows; such contracts are skipped by overlap checks.
	LeaseStart string `json:"lease_start,omitempty"`
	LeaseEnd   string `json:"lease_end,omitempty"`
	// TerminatedOn / TerminationReason are set only while status is
	// terminated. They are lifecycle metadata, not contract text, so they are
	// deliberately excluded from ContentSHA256 (a termination does not
	// invalidate an existing signature).
	TerminatedOn      string `json:"terminated_on,omitempty"`
	TerminationReason string `json:"termination_reason,omitempty"`
	TerminationNote   string `json:"termination_note,omitempty"`
	// ExpiryState is derived at read time (expiring / expired) for active
	// contracts with a lease end date. It is never stored.
	ExpiryState string         `json:"expiry_state,omitempty"`
	Amount      float64        `json:"amount"`
	Data        map[string]any `json:"data"`
	// SourceContractID records the contract this one was renewed from (B7).
	// It is provenance metadata: set once at creation, never changed, and
	// deliberately excluded from ContentSHA256 so an existing signature's
	// content version is not invalidated by the link alone.
	SourceContractID string    `json:"source_contract_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// FieldDefinition describes one configurable data field. Built-in fields are
// seeded by migrations and bound to first-class columns; user fields are
// stored in the entity's data JSON under Key.
type FieldDefinition struct {
	ID           string   `json:"id"`
	Entity       string   `json:"entity"`
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue string   `json:"default_value,omitempty"`
	Options      []string `json:"options,omitempty"`
	// Group 是合同表单的可选分组名（te-bzqhx）；空串表示未分组，按
	// 「其他约定」渲染。内置字段由界面固定归组，不读取该值。
	Group      string    `json:"group,omitempty"`
	InLedger   bool      `json:"in_ledger"`
	InTemplate bool      `json:"in_template"`
	BuiltIn    bool      `json:"built_in"`
	Archived   bool      `json:"archived"`
	SortOrder  int       `json:"sort_order"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// FieldChange is one field-level difference recorded in the change log.
type FieldChange struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

// ChangeLog records who changed what, when. Rows are append-only.
type ChangeLog struct {
	ID        string        `json:"id"`
	Entity    string        `json:"entity"`
	EntityID  string        `json:"entity_id"`
	Action    string        `json:"action"`
	Actor     string        `json:"actor"`
	Summary   string        `json:"summary"`
	Changes   []FieldChange `json:"changes"`
	CreatedAt time.Time     `json:"created_at"`
}

// YearCount is one row of the cross-year contract summary.
type YearCount struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// ContractDetail bundles a contract with its related records.
type ContractDetail struct {
	Contract   Contract         `json:"contract"`
	PartyA     *Party           `json:"party_a,omitempty"`
	PartyB     *Party           `json:"party_b,omitempty"`
	Asset      *Asset           `json:"asset,omitempty"`
	Template   *Template        `json:"template,omitempty"`
	Signatures []Signature      `json:"signatures"`
	Seals      []Seal           `json:"seals"`
	Documents  []OutputDocument `json:"documents"`
}

// Signature is a retained handwritten signature image and its metadata.
// A lan_qr row is bound to the contract and to the exact content version it
// was collected for (B5). An archive row (v0.2 D3=B) instead points at the
// party's long-lived archive signature: it is deliberately not bound to one
// contract content version, so later contract edits do not invalidate it.
type Signature struct {
	ID            string    `json:"id"`
	PartyID       string    `json:"party_id,omitempty"`
	ContractID    string    `json:"contract_id,omitempty"`
	SignerName    string    `json:"signer_name"`
	AssetPath     string    `json:"asset_path"` // path relative to the data directory
	SHA256        string    `json:"sha256"`
	ContentSHA256 string    `json:"content_sha256,omitempty"`
	Source        string    `json:"source,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	WidthPx       int       `json:"width_px"`
	HeightPx      int       `json:"height_px"`
	CapturedAt    time.Time `json:"captured_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// Signature sources. lan_qr is a one-time LAN QR capture (现场手写);
// archive is the party archive signature reused for a contract (档案复用).
const (
	SignatureSourceLANQR   = "lan_qr"
	SignatureSourceArchive = "archive"
)

// Seal is an imported official-seal image retained as an asset file. Only the
// path and digest live in the database; the image itself stays under the data
// directory. PartyID links a seal to the unit it belongs to. Contract links
// are stored separately, so one seal can be reused by many contracts.
type Seal struct {
	ID            string    `json:"id"`
	PartyID       string    `json:"party_id,omitempty"`
	Name          string    `json:"name"`
	AssetPath     string    `json:"asset_path"` // path relative to the data directory
	SHA256        string    `json:"sha256"`
	MIME          string    `json:"mime"`
	WidthPx       int       `json:"width_px"`
	HeightPx      int       `json:"height_px"`
	Note          string    `json:"note,omitempty"`
	ContractCount int       `json:"contract_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Template kinds. HTML templates are rendered by the built-in layout engine;
// PDF templates are imported from a user-provided Word document, converted to
// PDF and filled by overlaying data at user-calibrated positions (B3).
const (
	TemplateKindHTML = "html"
	TemplateKindPDF  = "pdf"
)

// Placement binding types for PDF templates.
const (
	PlacementField     = "field"
	PlacementSignature = "signature"
	PlacementSeal      = "seal"
)

// Placement party roles for signatures and seals.
const (
	PlacementPartyA = "party_a"
	PlacementPartyB = "party_b"
)

// Placement seal modes: contract picks the seal linked to the contract (or a
// party seal), seal pins one specific seal asset.
const (
	PlacementSealContract = "contract"
	PlacementSealFixed    = "seal"
)

// Placement is one calibrated box on a PDF template page. Coordinates are PDF
// points measured from the top-left corner of the page as displayed (rotation
// already applied); x,y is the top-left of the box, w,h its size. The editor
// draws exactly this rectangle over the PDF page.
type Placement struct {
	ID   string  `json:"id"`
	Page int     `json:"page"` // 1-based page number
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Type string  `json:"type"` // field | signature | seal
	// Field is the bound value key for field placements:
	//   contract.<field_key>  e.g. contract.contract_no, contract.data.area_sqm
	//   party_a.<field_key> / party_b.<field_key>  e.g. party_b.name
	Field string `json:"field,omitempty"`
	// Party selects the signature/seal party for signature and seal bindings.
	Party string `json:"party,omitempty"`
	// SealMode is contract (resolve from the contract) or seal (use SealID).
	SealMode string `json:"seal_mode,omitempty"`
	// SealID pins a specific seal asset when SealMode is seal.
	SealID string `json:"seal_id,omitempty"`
	// FontSize is the preferred text size in points (0 = default 10.5 pt).
	FontSize float64 `json:"font_size,omitempty"`
	// Align is the horizontal text alignment: left (default), center, right.
	Align string `json:"align,omitempty"`
}

// Template is a versioned contract template. HTML templates keep their body in
// HTML; PDF templates keep the imported Word file and converted PDF as assets
// and store the calibrated positions in Placements. Every saved modification
// creates a new row, so contracts can keep referencing the version they were
// generated with.
type Template struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   int      `json:"version"`
	Kind      string   `json:"kind,omitempty"` // html (default) | pdf
	HTML      string   `json:"html,omitempty"`
	Variables []string `json:"variables"`
	IsActive  bool     `json:"is_active"`
	// SourcePath is the original .docx asset path (relative to the data dir).
	SourcePath string `json:"source_path,omitempty"`
	// PDFPath is the converted template PDF asset path.
	PDFPath string `json:"pdf_path,omitempty"`
	// PageCount is the converted PDF page count.
	PageCount int `json:"page_count,omitempty"`
	// Converter records how the PDF was produced: word or libreoffice.
	Converter string `json:"converter,omitempty"`
	// SourceSHA256 and PDFSHA256 digest the imported source and converted PDF.
	SourceSHA256 string `json:"source_sha256,omitempty"`
	PDFSHA256    string `json:"pdf_sha256,omitempty"`
	// Placements holds the calibrated boxes for PDF templates.
	Placements []Placement `json:"placements,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// Output document kinds (B4): initial generation and re-output after a
// template version change / an intentional reissue.
const (
	DocumentKindInitial    = "initial"
	DocumentKindRegenerate = "regenerate"
)

// Output document statuses. Failed attempts keep the error so the UI can show
// a retry entry; they never point at a file.
const (
	DocumentStatusSuccess = "success"
	DocumentStatusFailed  = "failed"
)

// DocumentSignatureSnapshot pins the signatures used for one generated PDF.
// It lets users later see which signature asset was stamped into which output,
// whether it was collected on site or reused from the party archive, and (for
// on-site captures) the content version it was collected for.
type DocumentSignatureSnapshot struct {
	SignatureID   string `json:"signature_id"`
	PartyID       string `json:"party_id,omitempty"`
	SignerName    string `json:"signer_name,omitempty"`
	SHA256        string `json:"sha256"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	// Source is lan_qr (现场手写) or archive (档案复用).
	Source string `json:"source,omitempty"`
}

// OutputDocument records one PDF generation attempt (B4): who ran it, when,
// with which template version, over which contract content version, and which
// signatures were stamped. Successful rows point at a file under outputs/;
// failed rows keep the error message for retry.
type OutputDocument struct {
	ID              string `json:"id"`
	ContractID      string `json:"contract_id,omitempty"`
	TemplateID      string `json:"template_id,omitempty"`
	TemplateVersion int    `json:"template_version"`
	FilePath        string `json:"file_path"` // path relative to the data directory
	SHA256          string `json:"sha256"`
	PageCount       int    `json:"page_count"`
	// ContentSHA256 is the contract content digest at generation time.
	ContentSHA256 string `json:"content_sha256,omitempty"`
	// Signatures snapshots the signature assets used by signature placements.
	Signatures []DocumentSignatureSnapshot `json:"signatures"`
	// Operator is the audit actor (desktop user or tengshou-mcp).
	Operator string `json:"operator,omitempty"`
	// Kind is initial (first output) or regenerate (reissue after a template
	// version change or an explicit regeneration).
	Kind string `json:"kind"`
	// Status is success or failed.
	Status string `json:"status"`
	// ErrorMessage carries the failure reason for failed attempts.
	ErrorMessage string `json:"error_message,omitempty"`
	// BatchID groups documents produced by one batch run.
	BatchID string `json:"batch_id,omitempty"`
	// SourceDocumentID points at the document replaced by a regenerate.
	SourceDocumentID string    `json:"source_document_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// TemplateUsage summarizes how widely a template family is referenced. It is
// shown before saving a new version so users see the impact of template
// changes on already generated contracts.
type TemplateUsage struct {
	TemplateID     string `json:"template_id"`
	Name           string `json:"name"`
	Version        int    `json:"version"`
	LatestVersion  int    `json:"latest_version"`
	FamilyVersions int    `json:"family_versions"`
	// VersionContracts count contracts pinned to this exact version.
	VersionContracts int `json:"version_contracts"`
	// FamilyContracts count contracts pinned to any version of this name.
	FamilyContracts int `json:"family_contracts"`
	// OtherVersionContracts count contracts that still use another version.
	OtherVersionContracts int `json:"other_version_contracts"`
	// Documents count generated PDFs produced from any version of this name.
	Documents int `json:"documents"`
}

// Stats summarizes the local dataset.
type Stats struct {
	Parties    int `json:"parties"`
	Contracts  int `json:"contracts"`
	Templates  int `json:"templates"`
	Signatures int `json:"signatures"`
	Seals      int `json:"seals"`
	Documents  int `json:"documents"`
	Fields     int `json:"fields"`
	// ExpiringContracts / ExpiredContracts count active contracts whose lease
	// ends within ExpiryReminderDays / has already ended. They are reminders
	// only: nothing is changed automatically.
	ExpiringContracts int `json:"expiring_contracts"`
	ExpiredContracts  int `json:"expired_contracts"`
}

// List is the JSON envelope for paginated collections.
type List[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ListFilter is shared by list endpoints.
type ListFilter struct {
	Limit  int
	Offset int
	Query  string
	Status string
	// Year limits contracts to one contract year; 0 means all years.
	Year int
	// ContractID limits signature assets or output documents to one contract;
	// empty means all.
	ContractID string
	// TemplateID limits output documents to one template version.
	TemplateID string
	// BatchID limits output documents to one batch run.
	BatchID string
	// PartyID limits seal assets to one party, and contracts to records where
	// either side references the party (B7 archive history); empty means all.
	PartyID string
	// Kind limits templates to html or pdf; empty means both.
	Kind string
}

// Normalize clamps pagination to safe bounds.
func (f *ListFilter) Normalize() {
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

// NewID returns a random identifier with the given prefix.
func NewID(prefix string) (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// Now returns the current time truncated to seconds in UTC.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
