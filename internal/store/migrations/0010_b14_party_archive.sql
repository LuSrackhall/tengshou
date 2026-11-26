-- B14 v0.2 tenant archive upgrade: ID-card photos and an archive-level
-- handwritten signature.
--
-- The ID photos are compressed JPEG assets under the private data directory;
-- only paths and digests live in the database (same pattern as signatures).
-- signature_asset_id points at signature_assets.id: the archive signature is
-- captured once and can be reused by later contracts after explicit
-- confirmation (decision D3).
--
-- Existing archives keep empty id_number; the "required on creation" rule is
-- enforced by the application layer so backfilling old records stays possible.

ALTER TABLE parties ADD COLUMN id_front_path TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN id_front_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN id_back_path TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN id_back_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN signature_asset_id TEXT NOT NULL DEFAULT '';

-- Duplicate detection is keyed by ID number (Chinese ID / unified social
-- credit code). The index also serves the archive search.
CREATE INDEX IF NOT EXISTS idx_parties_id_number ON parties (id_number);

-- Note: the "required on creation" rule is enforced by the archive HTTP
-- surface, not by field_definitions.required. Making the built-in field
-- required here would also block contract generation for legacy archives
-- that have not backfilled their ID number yet.
