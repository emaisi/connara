package store

import (
	"apihub-go/internal/jsonutil"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListSyncTasks(ctx context.Context) ([]model.SyncTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT st.id::text, st.task_key, st.name, st.integration_id::text, i.integration_key,
		       st.connection_id::text, st.action_id::text, a.action_key, st.status,
		       st.schedule_type, COALESCE(st.cron_expression, ''), st.schedule_timezone,
		       st.next_run_at, st.retry_policy, st.input, st.sync_config,
		       COALESCE(ss.checkpoint, '{}'::jsonb), ss.last_success_at, ss.last_error_at,
		       COALESCE(ss.records_active, 0), st.version, st.created_at, st.updated_at
		FROM sync_tasks st
		JOIN integrations i ON i.id = st.integration_id AND i.workspace_id = st.workspace_id
		JOIN actions a ON a.id = st.action_id AND a.workspace_id = st.workspace_id
		LEFT JOIN sync_task_states ss ON ss.sync_task_id = st.id AND ss.workspace_id = st.workspace_id
		WHERE st.workspace_id = $1 AND st.deleted_at IS NULL
		ORDER BY st.updated_at DESC, st.id DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SyncTask, 0)
	for rows.Next() {
		item, err := scanSyncTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SyncTask(ctx context.Context, idOrKey string) (model.SyncTask, error) {
	item, err := scanSyncTask(s.pool.QueryRow(ctx, `
		SELECT st.id::text, st.task_key, st.name, st.integration_id::text, i.integration_key,
		       st.connection_id::text, st.action_id::text, a.action_key, st.status,
		       st.schedule_type, COALESCE(st.cron_expression, ''), st.schedule_timezone,
		       st.next_run_at, st.retry_policy, st.input, st.sync_config,
		       COALESCE(ss.checkpoint, '{}'::jsonb), ss.last_success_at, ss.last_error_at,
		       COALESCE(ss.records_active, 0), st.version, st.created_at, st.updated_at
		FROM sync_tasks st
		JOIN integrations i ON i.id = st.integration_id AND i.workspace_id = st.workspace_id
		JOIN actions a ON a.id = st.action_id AND a.workspace_id = st.workspace_id
		LEFT JOIN sync_task_states ss ON ss.sync_task_id = st.id AND ss.workspace_id = st.workspace_id
		WHERE st.workspace_id = $1 AND st.deleted_at IS NULL
		  AND (st.id::text = $2 OR st.task_key = $2)`, s.workspaceID, idOrKey))
	return item, mapNotFound(err)
}

func scanSyncTask(row rowScanner) (model.SyncTask, error) {
	var item model.SyncTask
	err := row.Scan(
		&item.ID, &item.TaskKey, &item.Name, &item.IntegrationID, &item.IntegrationKey,
		&item.ConnectionID, &item.ActionID, &item.ActionKey, &item.Status,
		&item.ScheduleType, &item.CronExpression, &item.ScheduleTimezone,
		&item.NextRunAt, &item.RetryPolicy, &item.Input, &item.SyncConfig, &item.Checkpoint,
		&item.LastSuccessAt, &item.LastErrorAt, &item.RecordsActive,
		&item.Version, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) SaveSyncTask(ctx context.Context, item model.SyncTask) (model.SyncTask, error) {
	if item.ID == "" {
		item.ID = StableID("sync-task", s.workspaceID+":"+item.TaskKey)
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	if item.ScheduleTimezone == "" {
		item.ScheduleTimezone = "UTC"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.validateSyncTaskDependencies(ctx, tx, item); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sync_tasks(
				id, workspace_id, task_key, name, integration_id, connection_id, action_id,
				status, schedule_type, cron_expression, schedule_timezone, next_run_at,
				retry_policy, input, sync_config
			) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, $15)
			ON CONFLICT(id) DO UPDATE SET
				task_key = EXCLUDED.task_key, name = EXCLUDED.name,
				integration_id = EXCLUDED.integration_id, connection_id = EXCLUDED.connection_id,
				action_id = EXCLUDED.action_id, status = EXCLUDED.status,
				schedule_type = EXCLUDED.schedule_type, cron_expression = EXCLUDED.cron_expression,
				schedule_timezone = EXCLUDED.schedule_timezone, next_run_at = EXCLUDED.next_run_at,
				retry_policy = EXCLUDED.retry_policy, input = EXCLUDED.input,sync_config=EXCLUDED.sync_config,
				version = sync_tasks.version + 1, updated_at = now()
			WHERE sync_tasks.workspace_id = EXCLUDED.workspace_id`,
			item.ID, s.workspaceID, item.TaskKey, item.Name, item.IntegrationID,
			item.ConnectionID, item.ActionID, item.Status, item.ScheduleType,
			item.CronExpression, item.ScheduleTimezone, nullTime(item.NextRunAt),
			jsonOrObject(item.RetryPolicy), jsonOrObject(item.Input), jsonOrObject(item.SyncConfig)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO sync_task_states(sync_task_id, workspace_id)
			VALUES($1, $2) ON CONFLICT(sync_task_id) DO NOTHING`, item.ID, s.workspaceID)
		return err
	})
	if err != nil {
		return model.SyncTask{}, err
	}
	return s.SyncTask(ctx, item.ID)
}

type syncDependencyQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) validateSyncTaskDependencies(ctx context.Context, querier syncDependencyQuerier, item model.SyncTask) error {
	var integrationSystemID, integrationStatus string
	if err := querier.QueryRow(ctx, `
		SELECT system_id::text, status FROM integrations
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
		s.workspaceID, item.IntegrationID).Scan(&integrationSystemID, &integrationStatus); err != nil {
		return mapNotFound(err)
	}
	if integrationStatus != "ready" {
		return fmt.Errorf("%w: sync integration is not ready", ErrConflict)
	}
	var connectionIntegrationID, connectionStatus string
	if err := querier.QueryRow(ctx, `
		SELECT integration_id::text, status FROM connections
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
		s.workspaceID, item.ConnectionID).Scan(&connectionIntegrationID, &connectionStatus); err != nil {
		return mapNotFound(err)
	}
	if connectionIntegrationID != item.IntegrationID {
		return fmt.Errorf("%w: sync connection belongs to another integration", ErrConflict)
	}
	if connectionStatus != "active" {
		return fmt.Errorf("%w: sync connection is not active", ErrConflict)
	}
	var actionSystemID, actionStatus string
	var actionExecutable bool
	if err := querier.QueryRow(ctx, `
		SELECT system_id::text, status, executable FROM actions
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
		s.workspaceID, item.ActionID).Scan(&actionSystemID, &actionStatus, &actionExecutable); err != nil {
		return mapNotFound(err)
	}
	if actionSystemID != integrationSystemID {
		return fmt.Errorf("%w: sync action belongs to another system", ErrConflict)
	}
	if actionStatus != "active" || !actionExecutable {
		return fmt.Errorf("%w: sync action is not executable", ErrConflict)
	}
	return nil
}

func (s *Store) ValidateSyncTaskReady(ctx context.Context, item model.SyncTask) error {
	return s.validateSyncTaskDependencies(ctx, s.pool, item)
}

func (s *Store) SetSyncTaskStatus(ctx context.Context, id, status string, nextRunAt *time.Time) (model.SyncTask, error) {
	command, err := s.pool.Exec(ctx, `
		UPDATE sync_tasks SET status = $3, next_run_at = $4,
		       version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
		s.workspaceID, id, status, nullTime(nextRunAt))
	if err != nil {
		return model.SyncTask{}, err
	}
	if command.RowsAffected() == 0 {
		return model.SyncTask{}, ErrNotFound
	}
	return s.SyncTask(ctx, id)
}

func (s *Store) DeleteSyncTask(ctx context.Context, id string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE sync_tasks SET status = 'disabled', deleted_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) EnqueueSync(ctx context.Context, task model.SyncTask, source, requestID string) (model.OperationRun, error) {
	var operation model.OperationRun
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM sync_tasks WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, s.workspaceID, task.ID); err != nil {
			return err
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE workspace_id=$1 AND kind='sync_run' AND resource_id=$2 AND status IN ('queued','running'))`, s.workspaceID, task.ID).Scan(&active); err != nil {
			return err
		}
		if active {
			return fmt.Errorf("%w: sync already queued or running", ErrConflict)
		}
		var err error
		operation, err = s.enqueueSync(ctx, tx, task, source, requestID)
		return err
	})
	return operation, err
}

func (s *Store) enqueueSync(ctx context.Context, tx pgx.Tx, task model.SyncTask, source, requestID string) (model.OperationRun, error) {
	now := utcNow()
	expiresAt, err := s.OperationExpiresAt(ctx, now)
	if err != nil {
		return model.OperationRun{}, err
	}
	operation := model.OperationRun{
		ID: StableID("operation", requestID), RequestID: requestID, Kind: "sync", Name: task.Name,
		Status: "queued", IntegrationID: task.IntegrationID, ConnectionID: task.ConnectionID,
		ActionID: task.ActionID, SyncTaskID: task.ID, Source: source, Input: task.Input,
		StartedAt: now, ExpiresAt: expiresAt,
	}
	jobID := StableID("job", requestID)
	maxAttempts := 3
	var retryPolicy struct {
		MaxAttempts int `json:"maxAttempts"`
	}
	if jsonutil.Unmarshal(task.RetryPolicy, &retryPolicy) == nil && retryPolicy.MaxAttempts >= 1 && retryPolicy.MaxAttempts <= 8 {
		maxAttempts = retryPolicy.MaxAttempts
	}
	err = func() error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO operation_runs(
				id, workspace_id, request_id, kind, name, status, integration_id,
				connection_id, action_id, sync_task_id, source, input, started_at, expires_at
			) VALUES($1, $2, $3, 'sync', $4, 'queued', $5, $6, $7, $8, $9, $10, $11, $12)`,
			operation.ID, s.workspaceID, requestID, task.Name, task.IntegrationID,
			task.ConnectionID, task.ActionID, task.ID, source, nilIfEmptyJSON(task.Input), now, operation.ExpiresAt); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"syncTaskId": task.ID})
		_, err := tx.Exec(ctx, `
			INSERT INTO jobs(id, workspace_id, kind, resource_id, operation_id, payload, status, max_attempts, run_after)
			VALUES($1, $2, 'sync_run', $3, $4, $5, 'queued', $6, now())`,
			jobID, s.workspaceID, task.ID, operation.ID, payload, maxAttempts)
		return err
	}()
	return operation, err
}

func (s *Store) RecordSyncFailure(ctx context.Context, taskID string) {
	_, _ = s.pool.Exec(ctx, `
		UPDATE sync_task_states SET last_error_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND sync_task_id = $2`, s.workspaceID, taskID)
}

func (s *Store) ClaimJobs(ctx context.Context, worker string, limit int) ([]model.Job, error) {
	limit = 1 // This worker executes serially; never lease more work than capacity.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.workspaceID+":claim-jobs"); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM jobs
			WHERE workspace_id = $1 AND attempt < max_attempts
 AND NOT EXISTS(SELECT 1 FROM jobs other WHERE other.workspace_id=$1 AND other.kind=jobs.kind AND other.resource_id=jobs.resource_id AND other.id<>jobs.id AND other.status='running' AND (other.lease_expires_at>now() OR other.id<jobs.id)) AND (
				(status = 'queued' AND run_after <= now()) OR
				(status = 'running' AND lease_expires_at < now())
			)
			ORDER BY priority DESC, run_after, id
			FOR UPDATE SKIP LOCKED LIMIT $2
		)
		UPDATE jobs j SET status = 'running', attempt = attempt + 1,
		       lease_owner = $3, lease_expires_at = now() + interval '60 seconds', updated_at = now()
		FROM claimed WHERE j.id = claimed.id
		RETURNING j.id::text, j.workspace_id::text, j.kind,
		          COALESCE(j.resource_id::text, ''), COALESCE(j.operation_id::text, ''),
		          j.payload, j.status, j.priority, j.attempt, j.max_attempts,
		          j.run_after, COALESCE(j.lease_owner, ''), j.lease_expires_at,
		          COALESCE(j.last_error, ''), j.created_at, j.updated_at`, s.workspaceID, limit, worker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Job, 0)
	for rows.Next() {
		var item model.Job
		if err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.Kind, &item.ResourceID, &item.OperationID,
			&item.Payload, &item.Status, &item.Priority, &item.Attempt, &item.MaxAttempts,
			&item.RunAfter, &item.LeaseOwner, &item.LeaseExpiresAt, &item.LastError,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return items, tx.Commit(ctx)
}

func (s *Store) CompleteJob(ctx context.Context, job model.Job, worker string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status = 'succeeded', completed_at = now(), lease_owner = NULL,
		       lease_expires_at = NULL, updated_at = now()
		WHERE id = $1 AND lease_owner = $2 AND status = 'running' AND attempt=$3 AND lease_expires_at>now() AND workspace_id=$4`, job.ID, worker, job.Attempt, s.workspaceID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) FailJob(ctx context.Context, job model.Job, worker, message string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.fenceJob(ctx, tx, job); err != nil {
			return err
		}
		if job.Kind == "sync_run" {
			if _, err := tx.Exec(ctx, `UPDATE sync_task_states SET last_error_at=now(),updated_at=now() WHERE workspace_id=$1 AND sync_task_id=$2`, s.workspaceID, job.ResourceID); err != nil {
				return err
			}
		}
		state := "queued"
		if job.Attempt >= job.MaxAttempts {
			state = "dead"
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status=$3,run_after=now()+$4*interval '1 second',last_error=$5,lease_owner=NULL,lease_expires_at=NULL,updated_at=now(),completed_at=CASE WHEN $3='dead' THEN now() END WHERE workspace_id=$1 AND id=$2`, s.workspaceID, job.ID, state, 1<<min(job.Attempt, 10), message); err != nil {
			return err
		}
		if job.OperationID != "" {
			status := "queued"
			if state == "dead" {
				status = "failed"
			}
			_, err := tx.Exec(ctx, `UPDATE operation_runs SET status=$3,error_message=$4,completed_at=CASE WHEN $3='failed' THEN now() END WHERE workspace_id=$1 AND id=$2 AND status<>'success'`, s.workspaceID, job.OperationID, status, message)
			return err
		}
		return nil
	})
}

func (s *Store) fenceJob(ctx context.Context, tx pgx.Tx, job model.Job) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM jobs WHERE workspace_id=$1 AND id=$2 AND lease_owner=$3 AND attempt=$4 AND status='running' AND lease_expires_at>now() FOR UPDATE`, s.workspaceID, job.ID, job.LeaseOwner, job.Attempt).Scan(&id)
	if err == pgx.ErrNoRows {
		return ErrConflict
	}
	return err
}
func (s *Store) RenewJob(ctx context.Context, job model.Job) error {
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=now()+interval '60 seconds',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND lease_owner=$3 AND attempt=$4 AND status='running' AND lease_expires_at>now()`, s.workspaceID, job.ID, job.LeaseOwner, job.Attempt)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func (s *Store) CommitSyncPage(ctx context.Context, job model.Job, task model.SyncTask, checkpoint []byte, records []model.SyncRecord, done bool, total int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.fenceJob(ctx, tx, job); err != nil {
			return err
		}
		var version int64
		if err := tx.QueryRow(ctx, `SELECT version FROM sync_tasks WHERE workspace_id=$1 AND id=$2 AND status='deployed' AND deleted_at IS NULL FOR SHARE`, s.workspaceID, task.ID).Scan(&version); err != nil {
			return mapNotFound(err)
		}
		if version != task.Version {
			return ErrConflict
		}
		tag, err := tx.Exec(ctx, `UPDATE sync_task_states SET checkpoint=$3,checkpoint_version=checkpoint_version+1,last_operation_id=$4,records_seen=records_seen+$5,updated_at=now() WHERE workspace_id=$1 AND sync_task_id=$2 AND checkpoint=$6::jsonb`, s.workspaceID, task.ID, jsonOrObject(checkpoint), job.OperationID, len(records), jsonOrObject(task.Checkpoint))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		batch := &pgx.Batch{}
		for _, record := range records {
			if record.ExternalID == "" {
				return fmt.Errorf("record has no stable external ID")
			}
			sum := sha256.Sum256(record.Payload)
			id := StableID("sync-record", task.ID+":"+record.Model+":"+record.ExternalID)
			batch.Queue(`INSERT INTO sync_records(id,workspace_id,sync_task_id,model,external_id,payload,payload_hash) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(workspace_id,sync_task_id,model,external_id) DO UPDATE SET payload=EXCLUDED.payload,payload_hash=EXCLUDED.payload_hash,last_seen_at=now(),deleted_at=NULL`, id, s.workspaceID, task.ID, record.Model, record.ExternalID, record.Payload, hex.EncodeToString(sum[:]))
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sync_task_states SET records_active=(SELECT count(*) FROM sync_records WHERE workspace_id=$1 AND sync_task_id=$2 AND deleted_at IS NULL),last_success_at=CASE WHEN $3 THEN now() ELSE last_success_at END WHERE workspace_id=$1 AND sync_task_id=$2`, s.workspaceID, task.ID, done); err != nil {
			return err
		}
		if !done {
			return nil
		}
		output := MarshalJSON(map[string]any{"records": total, "checkpoint": json.RawMessage(checkpoint)})
		if _, err := tx.Exec(ctx, `UPDATE operation_runs SET status='success',completed_at=now(),output=$3 WHERE workspace_id=$1 AND id=$2`, s.workspaceID, job.OperationID, output); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,workspace_id,event_type,aggregate_type,aggregate_id,dedupe_key,payload,status) VALUES($1,$2,'sync.completed','sync_task',$3,$4,$5,'pending') ON CONFLICT(workspace_id,dedupe_key) DO NOTHING`, StableID("outbox", "sync:"+job.OperationID), s.workspaceID, task.ID, "sync.completed:"+job.OperationID, MarshalJSON(map[string]any{"operationId": job.OperationID, "syncTaskId": task.ID, "records": total}))
		return err
	})
}

func (s *Store) ListSyncRecords(ctx context.Context, taskID string, limit int, options ...ListOptions) ([]model.SyncRecord, error) {
	o := listOptions(options)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, sync_task_id::text, model, external_id, payload, payload_hash,
		       source_created_at, source_updated_at, first_seen_at, last_seen_at, deleted_at
		FROM sync_records WHERE workspace_id = $1 AND sync_task_id = $2
 AND ($4::timestamptz IS NULL OR (first_seen_at,id)<($4,$5::uuid))
 AND ($6='' OR external_id ILIKE '%'||$6||'%')
		ORDER BY first_seen_at DESC, id DESC LIMIT $3`, s.workspaceID, taskID, limit, nullableTime(o.Before), nullableCursorID(o.ID), o.Query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SyncRecord, 0)
	for rows.Next() {
		var item model.SyncRecord
		if err := rows.Scan(&item.ID, &item.SyncTaskID, &item.Model, &item.ExternalID,
			&item.Payload, &item.PayloadHash, &item.SourceCreatedAt, &item.SourceUpdatedAt,
			&item.FirstSeenAt, &item.LastSeenAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWebhookEndpoints(ctx context.Context) ([]model.WebhookEndpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT we.id::text, we.name, we.primary_url, COALESCE(we.fallback_url, ''),
		       we.secret_blob, we.key_version, we.status, we.failure_count, we.paused_at,
		       COALESCE((SELECT array_agg(wee.event_pattern ORDER BY wee.event_pattern)
		                 FROM webhook_endpoint_events wee
		                 WHERE wee.workspace_id = we.workspace_id AND wee.webhook_endpoint_id = we.id), '{}'::text[]),
		       we.version, we.created_at, we.updated_at
		FROM webhook_endpoints we
		WHERE we.workspace_id = $1 AND we.deleted_at IS NULL
		ORDER BY we.updated_at DESC, we.id DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WebhookEndpoint, 0)
	for rows.Next() {
		var item model.WebhookEndpoint
		if err := rows.Scan(&item.ID, &item.Name, &item.PrimaryURL, &item.FallbackURL,
			&item.SecretBlob, &item.KeyVersion, &item.Status, &item.FailureCount, &item.PausedAt,
			&item.SubscribedEvents, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveWebhookEndpoint(ctx context.Context, item model.WebhookEndpoint) (model.WebhookEndpoint, error) {
	if item.ID == "" {
		item.ID = StableID("webhook-endpoint", s.workspaceID+":"+item.Name)
	}
	if item.Status == "" {
		item.Status = "active"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO webhook_endpoints(
				id, workspace_id, name, primary_url, fallback_url, secret_blob,
				key_version, status
			) VALUES($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8)
			ON CONFLICT(id) DO UPDATE SET
				name = EXCLUDED.name, primary_url = EXCLUDED.primary_url,
				fallback_url = EXCLUDED.fallback_url,
				secret_blob = CASE WHEN length(EXCLUDED.secret_blob) = 0 THEN webhook_endpoints.secret_blob ELSE EXCLUDED.secret_blob END,
				key_version = CASE WHEN length(EXCLUDED.secret_blob)=0 THEN webhook_endpoints.key_version ELSE EXCLUDED.key_version END, status = EXCLUDED.status,
				version = webhook_endpoints.version + 1, updated_at = now()
			WHERE webhook_endpoints.workspace_id = EXCLUDED.workspace_id`,
			item.ID, s.workspaceID, item.Name, item.PrimaryURL, item.FallbackURL,
			item.SecretBlob, item.KeyVersion, item.Status); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM webhook_endpoint_events WHERE workspace_id = $1 AND webhook_endpoint_id = $2`, s.workspaceID, item.ID); err != nil {
			return err
		}
		for _, event := range item.SubscribedEvents {
			if _, err := tx.Exec(ctx, `
				INSERT INTO webhook_endpoint_events(id, workspace_id, webhook_endpoint_id, event_pattern)
				VALUES($1, $2, $3, $4)`, StableID("webhook-event", item.ID+":"+event), s.workspaceID, item.ID, event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.WebhookEndpoint{}, err
	}
	items, err := s.ListWebhookEndpoints(ctx)
	if err != nil {
		return model.WebhookEndpoint{}, err
	}
	for _, current := range items {
		if current.ID == item.ID {
			return current, nil
		}
	}
	return model.WebhookEndpoint{}, ErrNotFound
}

func (s *Store) ListWebhookSources(ctx context.Context) ([]model.WebhookSource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ws.id::text, ws.source_key, ws.name, ws.integration_id::text, i.integration_key,
		       ws.status, ws.signature_type, ws.secret_blob, ws.key_version,
		       ws.subscribed_events, ws.settings, ws.version, ws.created_at, ws.updated_at
		FROM webhook_sources ws
		JOIN integrations i ON i.id = ws.integration_id AND i.workspace_id = ws.workspace_id
		WHERE ws.workspace_id = $1 AND ws.deleted_at IS NULL
		ORDER BY ws.updated_at DESC, ws.id DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WebhookSource, 0)
	for rows.Next() {
		var item model.WebhookSource
		if err := rows.Scan(&item.ID, &item.SourceKey, &item.Name, &item.IntegrationID,
			&item.IntegrationKey, &item.Status, &item.SignatureType, &item.SecretBlob,
			&item.KeyVersion, &item.SubscribedEvents, &item.Settings, &item.Version,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) WebhookSourceByKey(ctx context.Context, sourceKey string) (model.WebhookSource, error) {
	var item model.WebhookSource
	err := s.pool.QueryRow(ctx, `
		SELECT ws.id::text, ws.source_key, ws.name, ws.integration_id::text, i.integration_key,
		       ws.status, ws.signature_type, ws.secret_blob, ws.key_version,
		       ws.subscribed_events, ws.settings, ws.version, ws.created_at, ws.updated_at
		FROM webhook_sources ws
		JOIN integrations i ON i.id = ws.integration_id AND i.workspace_id = ws.workspace_id
		WHERE ws.workspace_id = $1 AND ws.source_key = $2 AND ws.deleted_at IS NULL`,
		s.workspaceID, sourceKey).Scan(&item.ID, &item.SourceKey, &item.Name, &item.IntegrationID,
		&item.IntegrationKey, &item.Status, &item.SignatureType, &item.SecretBlob,
		&item.KeyVersion, &item.SubscribedEvents, &item.Settings, &item.Version,
		&item.CreatedAt, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) RecordInboundWebhook(ctx context.Context, source model.WebhookSource, providerEventID, eventType string, payload, operationInput []byte, requestID string) (bool, string, error) {
	operationID := StableID("webhook-operation", source.ID+":"+providerEventID)
	eventID := StableID("webhook-ingress", source.ID+":"+providerEventID)
	now := utcNow()
	expiresAt, err := s.OperationExpiresAt(ctx, now)
	if err != nil {
		return false, "", err
	}
	created := false
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		command, err := tx.Exec(ctx, `
			INSERT INTO webhook_ingress_events(
				id, workspace_id, webhook_source_id, provider_event_id, event_type,
				payload, status, operation_id, processed_at, expires_at
			) VALUES($1, $2, $3, $4, $5, $6, 'processed', $7, $8, $9)
			ON CONFLICT(workspace_id, webhook_source_id, provider_event_id) DO NOTHING`,
			eventID, s.workspaceID, source.ID, providerEventID, eventType, payload,
			operationID, now, expiresAt)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return nil
		}
		created = true
		_, err = tx.Exec(ctx, `
			INSERT INTO operation_runs(
				id, workspace_id, request_id, kind, name, status, integration_id,
				source, input, output, started_at, completed_at, expires_at
			) VALUES($1, $2, $3, 'webhook', $4, 'success', $5, 'webhook', $6, $7, $8, $8, $9)`,
			operationID, s.workspaceID, requestID, source.Name, source.IntegrationID,
			operationInput, jsonOrObject(MarshalJSON(map[string]any{"eventType": eventType})), now,
			expiresAt)
		if err != nil {
			return err
		}
		outboxID := StableID("outbox", "inbound:"+eventID)
		_, err = tx.Exec(ctx, `
			INSERT INTO outbox_events(id, workspace_id, event_type, aggregate_type, aggregate_id, dedupe_key, payload, status)
			VALUES($1, $2, $3, 'webhook_ingress', $4, $5, $6, 'pending')
			ON CONFLICT(workspace_id, dedupe_key) DO NOTHING`, outboxID, s.workspaceID,
			eventType, eventID, "inbound:"+eventID, payload)
		return err
	})
	return created, operationID, err
}

func (s *Store) SaveWebhookSource(ctx context.Context, item model.WebhookSource) (model.WebhookSource, error) {
	if item.ID == "" {
		item.ID = StableID("webhook-source", s.workspaceID+":"+item.SourceKey)
	}
	if item.Status == "" {
		item.Status = "active"
	}
	var integrationStatus string
	if err := s.pool.QueryRow(ctx, `
		SELECT status FROM integrations
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
		s.workspaceID, item.IntegrationID).Scan(&integrationStatus); err != nil {
		return model.WebhookSource{}, mapNotFound(err)
	}
	if item.Status == "active" && integrationStatus != "ready" {
		return model.WebhookSource{}, fmt.Errorf("%w: active webhook sources require a ready integration", ErrConflict)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO webhook_sources(
			id, workspace_id, source_key, name, integration_id, status,
			signature_type, secret_blob, key_version, subscribed_events, settings
		) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT(id) DO UPDATE SET
			source_key = EXCLUDED.source_key, name = EXCLUDED.name,
			integration_id = EXCLUDED.integration_id, status = EXCLUDED.status,
			signature_type = EXCLUDED.signature_type,
			secret_blob = CASE WHEN EXCLUDED.secret_blob IS NULL THEN webhook_sources.secret_blob ELSE EXCLUDED.secret_blob END,
			key_version = CASE WHEN EXCLUDED.secret_blob IS NULL THEN webhook_sources.key_version ELSE EXCLUDED.key_version END, subscribed_events = EXCLUDED.subscribed_events,
			settings = EXCLUDED.settings, version = webhook_sources.version + 1, updated_at = now()
		WHERE webhook_sources.workspace_id = EXCLUDED.workspace_id`,
		item.ID, s.workspaceID, item.SourceKey, item.Name, item.IntegrationID,
		item.Status, item.SignatureType, item.SecretBlob, item.KeyVersion,
		item.SubscribedEvents, jsonOrObject(item.Settings))
	if err != nil {
		return model.WebhookSource{}, err
	}
	items, err := s.ListWebhookSources(ctx)
	if err != nil {
		return model.WebhookSource{}, err
	}
	for _, current := range items {
		if current.ID == item.ID {
			return current, nil
		}
	}
	return model.WebhookSource{}, ErrNotFound
}

func (s *Store) CreateOutboxEvent(ctx context.Context, event model.OutboxEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbox_events(
			id, workspace_id, event_type, aggregate_type, aggregate_id,
			dedupe_key, payload, status
		) VALUES($1, $2, $3, $4, $5, $6, $7, 'pending')
		ON CONFLICT(workspace_id, dedupe_key) DO NOTHING`, event.ID, s.workspaceID,
		event.EventType, event.AggregateType, event.AggregateID, event.DedupeKey,
		jsonOrObject(event.Payload))
	return err
}

// QueueWebhookTest creates exactly one delivery for the selected endpoint. Test
// events intentionally bypass subscription fan-out so a button press cannot
// notify unrelated endpoints and does not depend on a webhook.test pattern.
func (s *Store) QueueWebhookTest(ctx context.Context, endpointID string, event model.OutboxEvent) (string, error) {
	deliveryID := StableID("webhook-delivery", event.ID+":"+endpointID)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var targetURL, status string
		if err := tx.QueryRow(ctx, `
			SELECT primary_url, status FROM webhook_endpoints
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
			s.workspaceID, endpointID).Scan(&targetURL, &status); err != nil {
			return mapNotFound(err)
		}
		if status != "active" {
			return fmt.Errorf("%w: webhook endpoint is not active", ErrConflict)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO outbox_events(
				id, workspace_id, event_type, aggregate_type, aggregate_id,
				dedupe_key, payload, status, published_at
			) VALUES($1, $2, $3, $4, $5, $6, $7, 'published', now())`,
			event.ID, s.workspaceID, event.EventType, event.AggregateType,
			event.AggregateID, event.DedupeKey, jsonOrObject(event.Payload)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO webhook_deliveries(
				id, workspace_id, outbox_event_id, webhook_endpoint_id,
				target_url, status, next_attempt_at
			) VALUES($1, $2, $3, $4, $5, 'pending', now())`,
			deliveryID, s.workspaceID, event.ID, endpointID, targetURL); err != nil {
			return err
		}
		jobID := StableID("webhook-job", deliveryID)
		_, err := tx.Exec(ctx, `
			INSERT INTO jobs(id, workspace_id, kind, resource_id, payload, status, run_after)
			VALUES($1, $2, 'webhook_delivery', $3, '{}', 'queued', now())`,
			jobID, s.workspaceID, deliveryID)
		return err
	})
	return deliveryID, err
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, limit int, options ...ListOptions) ([]model.WebhookDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, `
		SELECT wd.id::text, oe.event_type, we.name, wd.target_url, wd.status,
		       wd.attempt_count, wd.next_attempt_at, COALESCE(wd.last_http_status, 0),
		       COALESCE(wd.last_error, ''), wd.created_at, wd.delivered_at
		FROM webhook_deliveries wd
		JOIN outbox_events oe ON oe.id = wd.outbox_event_id AND oe.workspace_id = wd.workspace_id
		JOIN webhook_endpoints we ON we.id = wd.webhook_endpoint_id AND we.workspace_id = wd.workspace_id
		WHERE wd.workspace_id = $1 AND ($3='' OR (wd.created_at,wd.id)<($4,NULLIF($3,'')::uuid))
 AND ($5='' OR wd.status=$5) AND ($6='' OR oe.event_type ILIKE '%'||$6||'%' OR we.name ILIKE '%'||$6||'%' OR wd.id::text=$6)
 AND ($7::timestamptz IS NULL OR wd.created_at >= $7) AND ($8::timestamptz IS NULL OR wd.created_at <= $8)
 ORDER BY wd.created_at DESC, wd.id DESC LIMIT $2`, s.workspaceID, limit, o.ID, o.Before, o.Status, o.Query, nullableTime(o.From), nullableTime(o.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		var item model.WebhookDelivery
		if err := rows.Scan(&item.ID, &item.EventType, &item.EndpointName, &item.TargetURL,
			&item.Status, &item.AttemptCount, &item.NextAttemptAt, &item.LastHTTPStatus,
			&item.LastError, &item.CreatedAt, &item.DeliveredAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWebhookDeliveryAttempts(ctx context.Context, deliveryID string) ([]model.WebhookDeliveryAttempt, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM webhook_deliveries WHERE workspace_id = $1 AND id = $2)`,
		s.workspaceID, deliveryID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, webhook_delivery_id::text, attempt_no, target_url, request_id,
		       COALESCE(http_status, 0), COALESCE(duration_ms, 0),
		       COALESCE(error_message, ''), created_at
		FROM webhook_delivery_attempts
		WHERE workspace_id = $1 AND webhook_delivery_id = $2
		ORDER BY attempt_no, created_at`, s.workspaceID, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WebhookDeliveryAttempt, 0)
	for rows.Next() {
		var item model.WebhookDeliveryAttempt
		if err := rows.Scan(&item.ID, &item.WebhookDeliveryID, &item.AttemptNo,
			&item.TargetURL, &item.RequestID, &item.HTTPStatus, &item.DurationMS,
			&item.ErrorMessage, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) WebhookEndpointSecretBlob(ctx context.Context, id string) ([]byte, error) {
	var blob []byte
	err := s.pool.QueryRow(ctx, `SELECT secret_blob FROM webhook_endpoints WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, s.workspaceID, id).Scan(&blob)
	return blob, mapNotFound(err)
}

func (s *Store) WebhookSourceSecretBlob(ctx context.Context, id string) ([]byte, error) {
	var blob []byte
	err := s.pool.QueryRow(ctx, `SELECT secret_blob FROM webhook_sources WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, s.workspaceID, id).Scan(&blob)
	return blob, mapNotFound(err)
}

func (s *Store) RetryWebhookDelivery(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var running bool
		if err := tx.QueryRow(ctx, `SELECT status='running' FROM jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, s.workspaceID, StableID("webhook-job", id)).Scan(&running); err != nil {
			return mapNotFound(err)
		}
		if running {
			return fmt.Errorf("%w: delivery is running", ErrConflict)
		}
		command, err := tx.Exec(ctx, `
			UPDATE webhook_deliveries SET status='pending', next_attempt_at=now(),
			       last_error=NULL, dead_at=NULL
			WHERE workspace_id=$1 AND id=$2 AND status IN ('retrying','dead')`, s.workspaceID, id)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM webhook_deliveries WHERE workspace_id=$1 AND id=$2)`, s.workspaceID, id).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("%w: only retrying or dead webhook deliveries can be retried", ErrConflict)
			}
			return ErrNotFound
		}
		jobID := StableID("webhook-job", id)
		_, err = tx.Exec(ctx, `
			INSERT INTO jobs(id,workspace_id,kind,resource_id,payload,status,run_after)
			VALUES($1,$2,'webhook_delivery',$3,'{}','queued',now())
			ON CONFLICT(id) DO UPDATE SET status='queued', attempt=0, run_after=now(),
				lease_owner=NULL, lease_expires_at=NULL, last_error=NULL,
				completed_at=NULL, updated_at=now()
			WHERE jobs.workspace_id=$2`, jobID, s.workspaceID, id)
		return err
	})
}

func MarshalJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
