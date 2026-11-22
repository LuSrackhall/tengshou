-- B13 seal image assets: imported official-seal pictures are kept as files
-- (path + sha256); the database only stores metadata. contract_seals links a
-- seal to any number of contracts so one picture can be reused when a contract
-- is re-rendered with another template.

CREATE TABLE IF NOT EXISTS seal_assets (
    id          TEXT PRIMARY KEY,
    party_id    TEXT REFERENCES parties (id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    asset_path  TEXT NOT NULL,
    sha256      TEXT NOT NULL,
    mime        TEXT NOT NULL DEFAULT 'image/png',
    width_px    INTEGER NOT NULL DEFAULT 0,
    height_px   INTEGER NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_seals_party ON seal_assets (party_id, created_at);
CREATE INDEX IF NOT EXISTS idx_seals_name ON seal_assets (name);

CREATE TABLE IF NOT EXISTS contract_seals (
    contract_id TEXT NOT NULL REFERENCES contracts (id) ON DELETE CASCADE,
    seal_id     TEXT NOT NULL REFERENCES seal_assets (id) ON DELETE CASCADE,
    placed_at   TEXT NOT NULL,
    PRIMARY KEY (contract_id, seal_id)
);

CREATE INDEX IF NOT EXISTS idx_contract_seals_seal ON contract_seals (seal_id);
