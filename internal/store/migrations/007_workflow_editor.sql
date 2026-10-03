BEGIN;
ALTER TABLE workflows ADD COLUMN editor_layout jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE workflows ADD COLUMN layout_version bigint NOT NULL DEFAULT 1 CHECK (layout_version > 0);
INSERT INTO schema_migrations(version) VALUES(7);
COMMIT;
