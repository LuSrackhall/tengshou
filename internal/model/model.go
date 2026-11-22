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

// Contract statuses.
const (
	ContractDraft    = "draft"
	ContractActive   = "active"
	ContractArchived = "archived"
)

// Party is a counterparty (person or organization) in a contract.
type Party struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	IDNumber  string         `json:"id_number,omitempty"`
	Phone     string         `json:"phone,omitempty"`
	Address   string         `json:"address,omitempty"`
	Note      string         `json:"note,omitempty"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Contract is one agreement between two parties.
type Contract struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	TemplateID string         `json:"template_id,omitempty"`
	PartyAID   string         `json:"party_a_id,omitempty"`
	PartyBID   string         `json:"party_b_id,omitempty"`
	Status     string         `json:"status"`
	SignedOn   string         `json:"signed_on,omitempty"`
	ValidFrom  string         `json:"valid_from,omitempty"`
	ValidTo    string         `json:"valid_to,omitempty"`
	Data       map[string]any `json:"data"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ContractDetail bundles a contract with its related records.
type ContractDetail struct {
	Contract   Contract         `json:"contract"`
	PartyA     *Party           `json:"party_a,omitempty"`
	PartyB     *Party           `json:"party_b,omitempty"`
	Template   *Template        `json:"template,omitempty"`
	Signatures []Signature      `json:"signatures"`
	Documents  []OutputDocument `json:"documents"`
}

// Signature is a retained handwritten signature image and its metadata.
type Signature struct {
	ID         string    `json:"id"`
	PartyID    string    `json:"party_id,omitempty"`
	SignerName string    `json:"signer_name"`
	AssetPath  string    `json:"asset_path"` // path relative to the data directory
	SHA256     string    `json:"sha256"`
	WidthPx    int       `json:"width_px"`
	HeightPx   int       `json:"height_px"`
	CapturedAt time.Time `json:"captured_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Template is a versioned HTML contract template.
type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Version   int       `json:"version"`
	HTML      string    `json:"html,omitempty"`
	Variables []string  `json:"variables"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OutputDocument records a rendered PDF.
type OutputDocument struct {
	ID              string    `json:"id"`
	ContractID      string    `json:"contract_id,omitempty"`
	TemplateID      string    `json:"template_id,omitempty"`
	TemplateVersion int       `json:"template_version"`
	FilePath        string    `json:"file_path"` // path relative to the data directory
	SHA256          string    `json:"sha256"`
	PageCount       int       `json:"page_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// Stats summarizes the local dataset.
type Stats struct {
	Parties    int `json:"parties"`
	Contracts  int `json:"contracts"`
	Templates  int `json:"templates"`
	Signatures int `json:"signatures"`
	Documents  int `json:"documents"`
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
