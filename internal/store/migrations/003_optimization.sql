BEGIN;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE operation_runs
    ADD COLUMN IF NOT EXISTS duration_ms bigint GENERATED ALWAYS AS (
        CASE WHEN completed_at IS NULL THEN NULL
             ELSE GREATEST(0, round(EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000))::bigint
        END
    ) STORED;
ALTER TABLE metric_hourly ADD COLUMN IF NOT EXISTS duration_count bigint NOT NULL DEFAULT 0;

CREATE INDEX operation_runs_metrics_idx
    ON operation_runs(workspace_id, started_at) INCLUDE(status, duration_ms)
    WHERE completed_at IS NOT NULL;
CREATE INDEX systems_search_trgm_idx ON systems USING gin(system_key gin_trgm_ops, name gin_trgm_ops, description gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX system_groups_search_trgm_idx ON system_groups USING gin(name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX auth_templates_search_trgm_idx ON auth_templates USING gin(name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX auth_instances_search_trgm_idx ON auth_instances USING gin(name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX integrations_search_trgm_idx ON integrations USING gin(integration_key gin_trgm_ops, name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX connections_search_trgm_idx ON connections USING gin(connection_key gin_trgm_ops, name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX end_users_search_trgm_idx ON end_users USING gin(external_key gin_trgm_ops);
CREATE INDEX actions_search_trgm_idx ON actions USING gin(action_key gin_trgm_ops, name gin_trgm_ops, description gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX operation_runs_search_trgm_idx ON operation_runs USING gin(name gin_trgm_ops);
CREATE INDEX audit_logs_search_trgm_idx ON audit_logs USING gin(actor_label gin_trgm_ops, action gin_trgm_ops, resource_label gin_trgm_ops);

ALTER TABLE user_sessions ADD CONSTRAINT user_sessions_user_fk FOREIGN KEY(user_id) REFERENCES users(id) NOT VALID;
ALTER TABLE user_sessions ADD CONSTRAINT user_sessions_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_user_fk FOREIGN KEY(user_id) REFERENCES users(id) NOT VALID;
ALTER TABLE workspace_invitations ADD CONSTRAINT workspace_invitations_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE workspace_invitations ADD CONSTRAINT workspace_invitations_inviter_fk FOREIGN KEY(invited_by) REFERENCES users(id) NOT VALID;
ALTER TABLE system_groups ADD CONSTRAINT system_groups_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE systems ADD CONSTRAINT systems_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE systems ADD CONSTRAINT systems_group_fk FOREIGN KEY(group_id) REFERENCES system_groups(id) NOT VALID;
ALTER TABLE auth_templates ADD CONSTRAINT auth_templates_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE system_auth_templates ADD CONSTRAINT system_auth_templates_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE system_auth_templates ADD CONSTRAINT system_auth_templates_system_fk FOREIGN KEY(system_id) REFERENCES systems(id) NOT VALID;
ALTER TABLE system_auth_templates ADD CONSTRAINT system_auth_templates_template_fk FOREIGN KEY(auth_template_id) REFERENCES auth_templates(id) NOT VALID;
ALTER TABLE auth_instances ADD CONSTRAINT auth_instances_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE auth_instances ADD CONSTRAINT auth_instances_system_fk FOREIGN KEY(system_id) REFERENCES systems(id) NOT VALID;
ALTER TABLE auth_instances ADD CONSTRAINT auth_instances_template_fk FOREIGN KEY(auth_template_id) REFERENCES auth_templates(id) NOT VALID;
ALTER TABLE integrations ADD CONSTRAINT integrations_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE integrations ADD CONSTRAINT integrations_system_fk FOREIGN KEY(system_id) REFERENCES systems(id) NOT VALID;
ALTER TABLE integrations ADD CONSTRAINT integrations_auth_instance_fk FOREIGN KEY(auth_instance_id) REFERENCES auth_instances(id) NOT VALID;
ALTER TABLE end_users ADD CONSTRAINT end_users_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE connections ADD CONSTRAINT connections_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE connections ADD CONSTRAINT connections_integration_fk FOREIGN KEY(integration_id) REFERENCES integrations(id) NOT VALID;
ALTER TABLE connections ADD CONSTRAINT connections_auth_instance_fk FOREIGN KEY(auth_instance_id) REFERENCES auth_instances(id) NOT VALID;
ALTER TABLE connections ADD CONSTRAINT connections_end_user_fk FOREIGN KEY(end_user_id) REFERENCES end_users(id) NOT VALID;
ALTER TABLE actions ADD CONSTRAINT actions_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE actions ADD CONSTRAINT actions_system_fk FOREIGN KEY(system_id) REFERENCES systems(id) NOT VALID;
ALTER TABLE actions ADD CONSTRAINT actions_integration_fk FOREIGN KEY(integration_id) REFERENCES integrations(id) NOT VALID;
ALTER TABLE runtime_tokens ADD CONSTRAINT runtime_tokens_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE runtime_tokens ADD CONSTRAINT runtime_tokens_creator_fk FOREIGN KEY(created_by) REFERENCES users(id) NOT VALID;
ALTER TABLE runtime_token_action_rules ADD CONSTRAINT runtime_token_action_rules_token_fk FOREIGN KEY(runtime_token_id) REFERENCES runtime_tokens(id) NOT VALID;
ALTER TABLE runtime_token_connection_grants ADD CONSTRAINT runtime_token_connection_grants_token_fk FOREIGN KEY(runtime_token_id) REFERENCES runtime_tokens(id) NOT VALID;
ALTER TABLE runtime_token_connection_grants ADD CONSTRAINT runtime_token_connection_grants_connection_fk FOREIGN KEY(connection_id) REFERENCES connections(id) NOT VALID;
ALTER TABLE sync_tasks ADD CONSTRAINT sync_tasks_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE sync_tasks ADD CONSTRAINT sync_tasks_integration_fk FOREIGN KEY(integration_id) REFERENCES integrations(id) NOT VALID;
ALTER TABLE sync_tasks ADD CONSTRAINT sync_tasks_connection_fk FOREIGN KEY(connection_id) REFERENCES connections(id) NOT VALID;
ALTER TABLE sync_tasks ADD CONSTRAINT sync_tasks_action_fk FOREIGN KEY(action_id) REFERENCES actions(id) NOT VALID;
ALTER TABLE sync_task_states ADD CONSTRAINT sync_task_states_task_fk FOREIGN KEY(sync_task_id) REFERENCES sync_tasks(id) NOT VALID;
ALTER TABLE sync_records ADD CONSTRAINT sync_records_task_fk FOREIGN KEY(sync_task_id) REFERENCES sync_tasks(id) NOT VALID;
ALTER TABLE jobs ADD CONSTRAINT jobs_operation_fk FOREIGN KEY(operation_id) REFERENCES operation_runs(id) NOT VALID;
ALTER TABLE webhook_endpoint_events ADD CONSTRAINT webhook_endpoint_events_endpoint_fk FOREIGN KEY(webhook_endpoint_id) REFERENCES webhook_endpoints(id) NOT VALID;
ALTER TABLE webhook_sources ADD CONSTRAINT webhook_sources_integration_fk FOREIGN KEY(integration_id) REFERENCES integrations(id) NOT VALID;
ALTER TABLE webhook_ingress_events ADD CONSTRAINT webhook_ingress_events_source_fk FOREIGN KEY(webhook_source_id) REFERENCES webhook_sources(id) NOT VALID;
ALTER TABLE webhook_ingress_events ADD CONSTRAINT webhook_ingress_events_operation_fk FOREIGN KEY(operation_id) REFERENCES operation_runs(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_outbox_fk FOREIGN KEY(outbox_event_id) REFERENCES outbox_events(id) NOT VALID;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_endpoint_fk FOREIGN KEY(webhook_endpoint_id) REFERENCES webhook_endpoints(id) NOT VALID;
ALTER TABLE webhook_delivery_attempts ADD CONSTRAINT webhook_delivery_attempts_delivery_fk FOREIGN KEY(webhook_delivery_id) REFERENCES webhook_deliveries(id) NOT VALID;
ALTER TABLE operation_events ADD CONSTRAINT operation_events_operation_fk FOREIGN KEY(operation_id) REFERENCES operation_runs(id) NOT VALID;
ALTER TABLE idempotency_records ADD CONSTRAINT idempotency_records_token_fk FOREIGN KEY(runtime_token_id) REFERENCES runtime_tokens(id) NOT VALID;
ALTER TABLE idempotency_records ADD CONSTRAINT idempotency_records_operation_fk FOREIGN KEY(operation_id) REFERENCES operation_runs(id) NOT VALID;
ALTER TABLE platform_settings ADD CONSTRAINT platform_settings_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE platform_secrets ADD CONSTRAINT platform_secrets_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE platform_secrets ADD CONSTRAINT platform_secrets_previous_fk FOREIGN KEY(rotated_from_id) REFERENCES platform_secrets(id) NOT VALID;
ALTER TABLE metric_hourly ADD CONSTRAINT metric_hourly_workspace_fk FOREIGN KEY(workspace_id) REFERENCES workspaces(id) NOT VALID;

ALTER TABLE workspaces ADD CONSTRAINT workspaces_status_check CHECK(status IN ('active','disabled')) NOT VALID;
ALTER TABLE users ADD CONSTRAINT users_status_check CHECK(status IN ('active','invited','disabled')) NOT VALID;
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_role_check CHECK(role IN ('owner','admin','developer','viewer')) NOT VALID;
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_status_check CHECK(status IN ('active','invited','disabled')) NOT VALID;
ALTER TABLE systems ADD CONSTRAINT systems_status_check CHECK(status IN ('active','disabled')) NOT VALID;
ALTER TABLE auth_templates ADD CONSTRAINT auth_templates_status_check CHECK(status IN ('draft','published','disabled')) NOT VALID;
ALTER TABLE auth_instances ADD CONSTRAINT auth_instances_status_check CHECK(status IN ('ready','draft','disabled')) NOT VALID;
ALTER TABLE integrations ADD CONSTRAINT integrations_status_check CHECK(status IN ('ready','draft','disabled')) NOT VALID;
ALTER TABLE connections ADD CONSTRAINT connections_status_check CHECK(status IN ('pending','active','error','disabled')) NOT VALID;
ALTER TABLE actions ADD CONSTRAINT actions_status_check CHECK(status IN ('active','draft','disabled')) NOT VALID;
ALTER TABLE runtime_tokens ADD CONSTRAINT runtime_tokens_status_check CHECK(status IN ('active','revoked')) NOT VALID;
ALTER TABLE runtime_token_action_rules ADD CONSTRAINT runtime_token_action_rules_effect_check CHECK(effect IN ('allow','deny')) NOT VALID;
ALTER TABLE sync_tasks ADD CONSTRAINT sync_tasks_status_check CHECK(status IN ('draft','deployed','paused','disabled')) NOT VALID;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check CHECK(status IN ('queued','running','succeeded','dead')) NOT VALID;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK(kind IN ('sync_run','webhook_delivery')) NOT VALID;
ALTER TABLE webhook_endpoints ADD CONSTRAINT webhook_endpoints_status_check CHECK(status IN ('active','paused','disabled')) NOT VALID;
ALTER TABLE webhook_sources ADD CONSTRAINT webhook_sources_status_check CHECK(status IN ('active','disabled')) NOT VALID;
ALTER TABLE webhook_ingress_events ADD CONSTRAINT webhook_ingress_events_status_check CHECK(status IN ('received','processed','ignored','failed')) NOT VALID;
ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_status_check CHECK(status IN ('pending','publishing','published','dead')) NOT VALID;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_status_check CHECK(status IN ('pending','retrying','delivered','dead')) NOT VALID;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_kind_check CHECK(kind IN ('action','sync','webhook','auth')) NOT VALID;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_status_check CHECK(status IN ('queued','running','success','failed','cancelled','unknown')) NOT VALID;
ALTER TABLE operation_events ADD CONSTRAINT operation_events_level_check CHECK(level IN ('debug','info','warn','error')) NOT VALID;
ALTER TABLE idempotency_records ADD CONSTRAINT idempotency_records_status_check CHECK(status IN ('claimed','running','completed','unknown')) NOT VALID;
ALTER TABLE platform_secrets ADD CONSTRAINT platform_secrets_status_check CHECK(status IN ('active','grace','revoked')) NOT VALID;

INSERT INTO schema_migrations(version) VALUES(3);
COMMIT;
