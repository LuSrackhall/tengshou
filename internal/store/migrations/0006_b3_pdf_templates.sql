-- B3 PDF templates: a template version may now be an imported Word document
-- converted to PDF and calibrated with position mappings. The original .docx
-- and the converted PDF stay as asset files; only their paths and digests are
-- stored. HTML templates keep kind='html' and an empty pdf_path.
--
-- mapping_json holds {"version":1,"placements":[{page,x,y,w,h,type,...}]};
-- coordinates are PDF points from the top-left of the visible page.

ALTER TABLE templates ADD COLUMN kind TEXT NOT NULL DEFAULT 'html';
ALTER TABLE templates ADD COLUMN source_path TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN pdf_path TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN mapping_json TEXT NOT NULL DEFAULT '{"version":1,"placements":[]}';
ALTER TABLE templates ADD COLUMN page_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE templates ADD COLUMN converter TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN source_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN pdf_sha256 TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_templates_kind_name ON templates (kind, name, version);
