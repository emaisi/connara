BEGIN;
ALTER TABLE actions ADD COLUMN request_config jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE actions ADD COLUMN response_config jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE actions ADD COLUMN execution_config jsonb NOT NULL DEFAULT '{}'::jsonb;
-- Legacy binding is retained for old readers, but new definitions never write it.
COMMENT ON COLUMN actions.integration_id IS 'Deprecated; execution target is supplied per invocation';
DROP INDEX actions_workspace_key_uq;
CREATE UNIQUE INDEX actions_system_key_uq ON actions(workspace_id,system_id,action_key) WHERE deleted_at IS NULL;
ALTER TABLE integrations ADD COLUMN target_version bigint NOT NULL DEFAULT 1;
ALTER TABLE connections ADD COLUMN enabled boolean NOT NULL DEFAULT true;
ALTER TABLE connections ADD COLUMN verified_target_version bigint NOT NULL DEFAULT 0;
ALTER TABLE connections ADD COLUMN verified_revision bigint NOT NULL DEFAULT 0;
INSERT INTO schema_migrations(version) VALUES(6);
COMMIT;
