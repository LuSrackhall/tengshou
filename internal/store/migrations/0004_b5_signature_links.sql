-- B5 LAN signature capture: link a retained signature asset to the contract it
-- was collected for and to the contract content version (SHA-256 over the
-- structured contract payload at capture time). source distinguishes capture
-- channels (lan_qr for the phone signing page).

ALTER TABLE signature_assets ADD COLUMN contract_id TEXT REFERENCES contracts (id) ON DELETE SET NULL;
ALTER TABLE signature_assets ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE signature_assets ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE signature_assets ADD COLUMN session_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_signatures_contract ON signature_assets (contract_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_signatures_content ON signature_assets (contract_id, content_sha256);
