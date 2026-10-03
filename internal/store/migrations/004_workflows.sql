BEGIN;

CREATE TABLE workflows (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    workflow_key varchar(120) NOT NULL,
    name varchar(180) NOT NULL,
    description varchar(1000) NOT NULL DEFAULT '',
    status varchar(24) NOT NULL,                    -- draft / deployed / paused / disabled
    graph jsonb NOT NULL DEFAULT '{"steps":[]}'::jsonb,
    schedule_type varchar(24) NOT NULL,             -- manual / interval / cron
    cron_expression varchar(120),
    schedule_timezone varchar(80) NOT NULL DEFAULT 'UTC',
    next_run_at timestamptz,
    retry_policy jsonb NOT NULL DEFAULT '{"maxAttempts":1}'::jsonb,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,       -- 默认触发输入：定时运行与空 body 触发的取值来源
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX workflows_workspace_key_uq
    ON workflows (workspace_id, workflow_key) WHERE deleted_at IS NULL;
CREATE INDEX workflows_due_idx
    ON workflows (next_run_at, id) WHERE status = 'deployed' AND deleted_at IS NULL;

ALTER TABLE operation_runs ADD COLUMN workflow_id uuid;
ALTER TABLE operation_runs ADD COLUMN workflow_version bigint;
ALTER TABLE operation_runs ADD COLUMN scheduled_for timestamptz;
ALTER TABLE operation_runs ADD COLUMN workflow_payload_ciphertext bytea;
ALTER TABLE operation_runs ADD COLUMN workflow_result_ciphertext bytea;
CREATE INDEX operation_runs_workflow_idx
    ON operation_runs (workspace_id, workflow_id, started_at DESC)
    WHERE workflow_id IS NOT NULL;
CREATE UNIQUE INDEX operation_runs_workflow_schedule_uq
    ON operation_runs (workspace_id, workflow_id, scheduled_for)
    WHERE source = 'schedule' AND workflow_id IS NOT NULL;

-- v3 的 NOT VALID 约束仍约束新行；必须在插入新 kind 前扩展。
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check
    CHECK (kind IN ('sync_run', 'webhook_delivery', 'workflow_run')) NOT VALID;
ALTER TABLE operation_runs DROP CONSTRAINT operation_runs_kind_check;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_kind_check
    CHECK (kind IN ('action', 'sync', 'webhook', 'auth', 'workflow')) NOT VALID;

ALTER TABLE workflows ADD CONSTRAINT workflows_workspace_fk
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_workflow_fk
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_status_check
    CHECK (status IN ('draft', 'deployed', 'paused', 'disabled')) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_schedule_check
    CHECK (schedule_type IN ('manual', 'interval', 'cron')) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_input_object_check
    CHECK (jsonb_typeof(input) = 'object') NOT VALID;

COMMENT ON TABLE workflows IS '由现有 API 操作构成的工作流定义';
COMMENT ON COLUMN workflows.id IS '工作流 UUID';
COMMENT ON COLUMN workflows.workspace_id IS '所属工作区 UUID';
COMMENT ON COLUMN workflows.workflow_key IS '稳定工作流标识';
COMMENT ON COLUMN workflows.name IS '工作流显示名称';
COMMENT ON COLUMN workflows.description IS '工作流用途说明';
COMMENT ON COLUMN workflows.status IS 'draft 草稿、deployed 可运行、paused 暂停、disabled 禁用';
COMMENT ON COLUMN workflows.graph IS '步骤、依赖、条件和最终输出映射';
COMMENT ON COLUMN workflows.schedule_type IS 'manual 手动、interval 固定间隔、cron 定时表达式';
COMMENT ON COLUMN workflows.cron_expression IS '固定间隔标记或五字段 cron 表达式';
COMMENT ON COLUMN workflows.schedule_timezone IS 'cron 使用的 IANA 时区';
COMMENT ON COLUMN workflows.next_run_at IS '下一次计划触发的 UTC 时间';
COMMENT ON COLUMN workflows.retry_policy IS '重试策略；默认最多执行一次';
COMMENT ON COLUMN workflows.input IS '定时运行及省略触发输入时的默认 JSON 对象；不得保存凭据';
COMMENT ON COLUMN workflows.version IS '每次定义或状态更新递增的版本号';
COMMENT ON COLUMN workflows.created_at IS '创建时间';
COMMENT ON COLUMN workflows.updated_at IS '最后更新时间';
COMMENT ON COLUMN workflows.deleted_at IS '软删除时间';
COMMENT ON COLUMN operation_runs.workflow_id IS '关联工作流 UUID';
COMMENT ON COLUMN operation_runs.workflow_version IS '触发时固定的工作流版本';
COMMENT ON COLUMN operation_runs.scheduled_for IS '计划触发的 UTC 时间；非定时运行为空';
COMMENT ON COLUMN operation_runs.workflow_payload_ciphertext IS '加密的运行定义快照及真实触发输入';
COMMENT ON COLUMN operation_runs.workflow_result_ciphertext IS '加密的最终输出，仅授权运行时查询解密';

INSERT INTO schema_migrations(version) VALUES(4);
COMMIT;
