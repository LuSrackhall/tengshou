-- Base schema for 腾手 v0.1 (B1 engineering foundation).
-- Conventions:
--   * ids are opaque prefixed strings (pty_, ctr_, tpl_, sig_, doc_)
--   * timestamps are RFC3339 UTC strings
--   * *_json columns hold opaque extension data for future dynamic fields (B2)

CREATE TABLE IF NOT EXISTS parties (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('person', 'organization')),
    name        TEXT NOT NULL,
    id_number   TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    data_json   TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_parties_name ON parties (name);

CREATE TABLE IF NOT EXISTS templates (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    html           TEXT NOT NULL,
    variables_json TEXT NOT NULL DEFAULT '[]',
    is_active      INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE (name, version)
);

CREATE TABLE IF NOT EXISTS contracts (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    template_id  TEXT REFERENCES templates (id) ON DELETE SET NULL,
    party_a_id   TEXT REFERENCES parties (id) ON DELETE SET NULL,
    party_b_id   TEXT REFERENCES parties (id) ON DELETE SET NULL,
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
    signed_on    TEXT NOT NULL DEFAULT '',
    valid_from   TEXT NOT NULL DEFAULT '',
    valid_to     TEXT NOT NULL DEFAULT '',
    data_json    TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_contracts_party_a ON contracts (party_a_id);
CREATE INDEX IF NOT EXISTS idx_contracts_party_b ON contracts (party_b_id);
CREATE INDEX IF NOT EXISTS idx_contracts_template ON contracts (template_id);

CREATE TABLE IF NOT EXISTS signature_assets (
    id          TEXT PRIMARY KEY,
    party_id    TEXT REFERENCES parties (id) ON DELETE SET NULL,
    signer_name TEXT NOT NULL DEFAULT '',
    asset_path  TEXT NOT NULL,
    sha256      TEXT NOT NULL,
    width_px    INTEGER NOT NULL DEFAULT 0,
    height_px   INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_signatures_party ON signature_assets (party_id);

CREATE TABLE IF NOT EXISTS output_documents (
    id               TEXT PRIMARY KEY,
    contract_id      TEXT REFERENCES contracts (id) ON DELETE SET NULL,
    template_id      TEXT REFERENCES templates (id) ON DELETE SET NULL,
    template_version INTEGER NOT NULL DEFAULT 0,
    file_path        TEXT NOT NULL,
    sha256           TEXT NOT NULL,
    page_count       INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_documents_contract ON output_documents (contract_id);
