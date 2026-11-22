-- B7 renewal provenance: a contract created from an existing contract keeps
-- the source contract id. The link is metadata only — it is not part of the
-- contract content digest, so it never invalidates retained signatures. The
-- column is written at creation time and ignored by updates.

ALTER TABLE contracts ADD COLUMN source_contract_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_contracts_source ON contracts (source_contract_id);
