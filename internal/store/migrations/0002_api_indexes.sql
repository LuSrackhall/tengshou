-- Secondary indexes used by the read-only data API and application metadata.

CREATE TABLE IF NOT EXISTS app_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_contracts_status ON contracts (status);
CREATE INDEX IF NOT EXISTS idx_contracts_created ON contracts (created_at);
CREATE INDEX IF NOT EXISTS idx_templates_name ON templates (name);
CREATE INDEX IF NOT EXISTS idx_documents_created ON output_documents (created_at);
