-- B4 contract generation: make output_documents a complete generation history.
-- Each row now records the contract content version, a snapshot of the
-- signatures that were stamped, the operator, whether it was the initial or a
-- regenerate/reissue, success/failure, the batch id and the source document of
-- a reissue. Existing rows keep working: they default to kind=initial,
-- status=success with empty new fields.

ALTER TABLE output_documents ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE output_documents ADD COLUMN signature_snapshot TEXT NOT NULL DEFAULT '[]';
ALTER TABLE output_documents ADD COLUMN operator TEXT NOT NULL DEFAULT '';
ALTER TABLE output_documents ADD COLUMN kind TEXT NOT NULL DEFAULT 'initial';
ALTER TABLE output_documents ADD COLUMN status TEXT NOT NULL DEFAULT 'success';
ALTER TABLE output_documents ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE output_documents ADD COLUMN batch_id TEXT NOT NULL DEFAULT '';
ALTER TABLE output_documents ADD COLUMN source_document_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_documents_template ON output_documents (template_id, created_at);
CREATE INDEX IF NOT EXISTS idx_documents_batch ON output_documents (batch_id);
