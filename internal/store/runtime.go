package store

import (
	"context"
	"fmt"
	"time"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateRuntimeToken(ctx context.Context, token model.RuntimeToken) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO runtime_tokens(
				id, workspace_id, name, token_prefix, token_hash, status,
				expires_at, created_by
			) VALUES($1, $2, $3, $4, $5, 'active', $6, $7)`,
			token.ID, s.workspaceID, token.Name, token.TokenPrefix, token.TokenHash,
			nullTime(token.ExpiresAt), token.CreatedBy); err != nil {
			return err
		}
		for _, pattern := range token.AllowedActions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO runtime_token_action_rules(id, workspace_id, runtime_token_id, effect, action_pattern)
				VALUES($1, $2, $3, 'allow', $4)`, StableID("token-rule", token.ID+":allow:"+pattern),
				s.workspaceID, token.ID, pattern); err != nil {
				return err
			}
		}
		for _, pattern := range token.BlockedActions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO runtime_token_action_rules(id, workspace_id, runtime_token_id, effect, action_pattern)
				VALUES($1, $2, $3, 'deny', $4)`, StableID("token-rule", token.ID+":deny:"+pattern),
				s.workspaceID, token.ID, pattern); err != nil {
				return err
			}
		}
		for _, connectionID := range token.AllowedConnections {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connections WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL)`, s.workspaceID, connectionID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: connection grant not found", ErrNotFound)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO runtime_token_connection_grants(id, workspace_id, runtime_token_id, connection_id)
				VALUES($1, $2, $3, $4)`, StableID("token-connection", token.ID+":"+connectionID),
				s.workspaceID, token.ID, connectionID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) RuntimeTokenByHash(ctx context.Context, hash string) (model.RuntimeToken, error) {
	var item model.RuntimeToken
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, workspace_id::text, name, token_prefix, token_hash, status,
		       expires_at, last_used_at, revoked_at, created_by::text, created_at,
 ARRAY(SELECT action_pattern FROM runtime_token_action_rules r WHERE r.workspace_id=runtime_tokens.workspace_id AND r.runtime_token_id=runtime_tokens.id AND r.effect='allow' ORDER BY action_pattern),
 ARRAY(SELECT action_pattern FROM runtime_token_action_rules r WHERE r.workspace_id=runtime_tokens.workspace_id AND r.runtime_token_id=runtime_tokens.id AND r.effect='deny' ORDER BY action_pattern),
 ARRAY(SELECT connection_id::text FROM runtime_token_connection_grants g WHERE g.workspace_id=runtime_tokens.workspace_id AND g.runtime_token_id=runtime_tokens.id ORDER BY connection_id)
		FROM runtime_tokens
		WHERE token_hash = $1 AND status = 'active' AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())`, hash).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.TokenPrefix, &item.TokenHash,
		&item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt,
		&item.CreatedBy, &item.CreatedAt, &item.AllowedActions, &item.BlockedActions, &item.AllowedConnections,
	)
	if err != nil {
		return model.RuntimeToken{}, mapNotFound(err)
	}
	if item.WorkspaceID != s.workspaceID {
		return model.RuntimeToken{}, ErrNotFound
	}
	now := utcNow()
	_, _ = s.pool.Exec(ctx, `UPDATE runtime_tokens SET last_used_at = $2 WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < $2-interval '1 minute')`, item.ID, now)
	item.LastUsedAt = &now
	return item, nil
}

func (s *Store) ListRuntimeTokens(ctx context.Context) ([]model.RuntimeToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, workspace_id::text, name, token_prefix, token_hash, status,
		       expires_at, last_used_at, revoked_at, created_by::text, created_at,
 ARRAY(SELECT action_pattern FROM runtime_token_action_rules r WHERE r.workspace_id=runtime_tokens.workspace_id AND r.runtime_token_id=runtime_tokens.id AND r.effect='allow' ORDER BY action_pattern),
 ARRAY(SELECT action_pattern FROM runtime_token_action_rules r WHERE r.workspace_id=runtime_tokens.workspace_id AND r.runtime_token_id=runtime_tokens.id AND r.effect='deny' ORDER BY action_pattern),
 ARRAY(SELECT connection_id::text FROM runtime_token_connection_grants g WHERE g.workspace_id=runtime_tokens.workspace_id AND g.runtime_token_id=runtime_tokens.id ORDER BY connection_id)
		FROM runtime_tokens WHERE workspace_id = $1 ORDER BY created_at DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.RuntimeToken, 0)
	for rows.Next() {
		var item model.RuntimeToken
		if err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.Name, &item.TokenPrefix, &item.TokenHash,
			&item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt,
			&item.CreatedBy, &item.CreatedAt, &item.AllowedActions, &item.BlockedActions, &item.AllowedConnections,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeRuntimeToken(ctx context.Context, id string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE runtime_tokens SET status = 'revoked', revoked_at = now()
		WHERE workspace_id = $1 AND id = $2 AND revoked_at IS NULL`, s.workspaceID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateOperation(ctx context.Context, run model.OperationRun) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operation_runs(
			id, workspace_id, request_id, kind, name, status,
			system_id, integration_id, connection_id, action_id, sync_task_id,
			runtime_token_id, source, input, started_at, expires_at
		) VALUES($1, $2, $3, $4, $5, $6,
		         NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid,
		         NULLIF($10, '')::uuid, NULLIF($11, '')::uuid, NULLIF($12, '')::uuid,
		         $13, $14, $15, $16)`,
		run.ID, s.workspaceID, run.RequestID, run.Kind, run.Name, run.Status,
		run.SystemID, run.IntegrationID, run.ConnectionID, run.ActionID, run.SyncTaskID,
		run.RuntimeTokenID, run.Source, nilIfEmptyJSON(run.Input), run.StartedAt, run.ExpiresAt)
	return err
}

func (s *Store) AddOperationEvent(ctx context.Context, operationID, level, message string, attributes []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operation_events(id, workspace_id, operation_id, sequence, level, message, attributes)
		SELECT $1, $2, $3, COALESCE(max(sequence), 0) + 1, $4, $5, $6
		FROM operation_events WHERE operation_id = $3`, StableID("operation-event", operationID+":"+time.Now().UTC().Format(time.RFC3339Nano)),
		s.workspaceID, operationID, level, message, jsonOrObject(attributes))
	return err
}

func (s *Store) CompleteOperation(ctx context.Context, id, status string, httpStatus int, output []byte, errorCode, errorMessage string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE operation_runs SET status = $3, http_status = NULLIF($4, 0),
		       output = $5, error_code = NULLIF($6, ''), error_message = NULLIF($7, ''),
		       completed_at = now()
		WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id, status, httpStatus,
		nilIfEmptyJSON(output), errorCode, errorMessage)
	return err
}

func (s *Store) ListOperations(ctx context.Context, kind, status, query string, limit int, options ...ListOptions) ([]model.OperationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, workspace_id::text, request_id, kind, name, status,
		       COALESCE(system_id::text, ''), COALESCE(integration_id::text, ''),
		       COALESCE(connection_id::text, ''), COALESCE(action_id::text, ''),
		       COALESCE(sync_task_id::text, ''), COALESCE(runtime_token_id::text, ''),
		       source, COALESCE(http_status, 0), input, output,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, completed_at, expires_at
		FROM operation_runs
		WHERE workspace_id = $1
		  AND ($2 = '' OR kind = $2)
		  AND ($3 = '' OR status = $3)
		  AND ($4 = '' OR name ILIKE '%' || $4 || '%' OR request_id ILIKE '%' || $4 || '%' OR id::text=$4
 OR EXISTS(SELECT 1 FROM connections c WHERE c.workspace_id=$1 AND c.id=operation_runs.connection_id AND (c.connection_key ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%'))
 OR EXISTS(SELECT 1 FROM integrations i WHERE i.workspace_id=$1 AND i.id=operation_runs.integration_id AND (i.integration_key ILIKE '%'||$4||'%' OR i.name ILIKE '%'||$4||'%')))
		AND ($6='' OR (started_at,id)<($7,NULLIF($6,'')::uuid))
 AND ($8='' OR integration_id::text=$8) AND ($9='' OR connection_id::text=$9) AND ($10='' OR sync_task_id::text=$10)
 AND ($11::timestamptz IS NULL OR started_at >= $11) AND ($12::timestamptz IS NULL OR started_at <= $12)
 ORDER BY started_at DESC, id DESC LIMIT $5`, s.workspaceID, kind, status, query, limit, o.ID, o.Before, o.IntegrationID, o.ConnectionID, o.SyncTaskID, nullableTime(o.From), nullableTime(o.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OperationRun, 0)
	for rows.Next() {
		item, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Operation(ctx context.Context, id string) (model.OperationRun, error) {
	item, err := scanOperation(s.pool.QueryRow(ctx, `
		SELECT id::text, workspace_id::text, request_id, kind, name, status,
		       COALESCE(system_id::text, ''), COALESCE(integration_id::text, ''),
		       COALESCE(connection_id::text, ''), COALESCE(action_id::text, ''),
		       COALESCE(sync_task_id::text, ''), COALESCE(runtime_token_id::text, ''),
		       source, COALESCE(http_status, 0), input, output,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, completed_at, expires_at
		FROM operation_runs WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id))
	if err != nil {
		return model.OperationRun{}, mapNotFound(err)
	}
	eventRows, err := s.pool.Query(ctx, `
		SELECT id::text, sequence, level, message, attributes, created_at
		FROM operation_events WHERE workspace_id = $1 AND operation_id = $2 ORDER BY sequence`, s.workspaceID, id)
	if err != nil {
		return model.OperationRun{}, err
	}
	defer eventRows.Close()
	item.Events = []model.OperationEvent{}
	for eventRows.Next() {
		var event model.OperationEvent
		if err := eventRows.Scan(&event.ID, &event.Sequence, &event.Level, &event.Message, &event.Attributes, &event.CreatedAt); err != nil {
			return model.OperationRun{}, err
		}
		item.Events = append(item.Events, event)
	}
	return item, eventRows.Err()
}

func scanOperation(row rowScanner) (model.OperationRun, error) {
	var item model.OperationRun
	err := row.Scan(
		&item.ID, &item.WorkspaceID, &item.RequestID, &item.Kind, &item.Name, &item.Status,
		&item.SystemID, &item.IntegrationID, &item.ConnectionID, &item.ActionID,
		&item.SyncTaskID, &item.RuntimeTokenID, &item.Source, &item.HTTPStatus,
		&item.Input, &item.Output, &item.ErrorCode, &item.ErrorMessage,
		&item.StartedAt, &item.CompletedAt, &item.ExpiresAt,
	)
	return item, err
}

func (s *Store) ClaimIdempotency(ctx context.Context, record model.IdempotencyRecord) (model.IdempotencyRecord, bool, error) {
	_, err := s.pool.Exec(ctx, `DELETE FROM idempotency_records WHERE workspace_id=$1 AND runtime_token_id=$2 AND scope=$3 AND idempotency_key=$4 AND status='completed' AND expires_at<=now()`, s.workspaceID, record.RuntimeTokenID, record.Scope, record.Key)
	if err != nil {
		return model.IdempotencyRecord{}, false, err
	}
	command, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_records(
			id, workspace_id, runtime_token_id, scope, idempotency_key,
			request_fingerprint, status, expires_at
		) VALUES($1, $2, $3, $4, $5, $6, 'claimed', $7)
		ON CONFLICT(workspace_id, runtime_token_id, scope, idempotency_key) DO NOTHING`,
		record.ID, s.workspaceID, record.RuntimeTokenID, record.Scope, record.Key,
		record.Fingerprint, record.ExpiresAt)
	if err != nil {
		return model.IdempotencyRecord{}, false, err
	}
	if command.RowsAffected() == 1 {
		record.WorkspaceID = s.workspaceID
		record.Status = "claimed"
		return record, true, nil
	}
	var current model.IdempotencyRecord
	var response []byte
	err = s.pool.QueryRow(ctx, `
		SELECT id::text, workspace_id::text, runtime_token_id::text, scope,
		       idempotency_key, request_fingerprint, status,
		       COALESCE(http_status, 0), response, COALESCE(operation_id::text, ''), expires_at
		FROM idempotency_records
		WHERE workspace_id = $1 AND runtime_token_id = $2 AND scope = $3 AND idempotency_key = $4`,
		s.workspaceID, record.RuntimeTokenID, record.Scope, record.Key).Scan(
		&current.ID, &current.WorkspaceID, &current.RuntimeTokenID, &current.Scope,
		&current.Key, &current.Fingerprint, &current.Status, &current.HTTPStatus,
		&response, &current.OperationID, &current.ExpiresAt)
	current.Response = response
	return current, false, err
}

func (s *Store) CompleteIdempotency(ctx context.Context, record model.IdempotencyRecord) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE idempotency_records SET status = 'completed', http_status = $5,
		       response = $6, operation_id = NULLIF($7, '')::uuid
		WHERE workspace_id = $1 AND runtime_token_id = $2 AND scope = $3 AND idempotency_key = $4`,
		s.workspaceID, record.RuntimeTokenID, record.Scope, record.Key,
		record.HTTPStatus, nilIfEmptyJSON(record.Response), record.OperationID)
	return err
}

func (s *Store) Metrics(ctx context.Context, since time.Time) (model.MetricsSummary, error) {
	var item model.MetricsSummary
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'success'),
			count(*) FILTER (WHERE status = 'failed'),
			COALESCE(avg(EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000) FILTER (WHERE completed_at IS NOT NULL), 0),
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000) FILTER (WHERE completed_at IS NOT NULL), 0),
			(SELECT count(*) FROM jobs WHERE workspace_id = $1 AND status IN ('queued', 'running')),
			(SELECT count(*) FROM connections WHERE workspace_id = $1 AND status = 'active' AND deleted_at IS NULL)
		FROM operation_runs WHERE workspace_id = $1 AND started_at >= $2`, s.workspaceID, since).Scan(
		&item.Requests, &item.Successes, &item.Failures, &item.AverageMS,
		&item.P95MS, &item.PendingJobs, &item.ActiveConnections)
	if item.Requests > 0 {
		item.SuccessRate = float64(item.Successes) / float64(item.Requests) * 100
	}
	return item, err
}

func nilIfEmptyJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (s *Store) ReleaseIdempotency(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM idempotency_records WHERE workspace_id=$1 AND id=$2 AND status='claimed'`, s.workspaceID, id)
	return err
}
func (s *Store) StartIdempotency(ctx context.Context, id, operationID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE idempotency_records SET status='running',operation_id=$3 WHERE workspace_id=$1 AND id=$2 AND status='claimed'`, s.workspaceID, id, operationID)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}
func (s *Store) FinishRuntime(ctx context.Context, runID, status string, httpStatus int, output []byte, code, message string, record model.IdempotencyRecord, response []byte) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE operation_runs SET status=$3,http_status=$4,output=$5,error_code=NULLIF($6,''),error_message=NULLIF($7,''),completed_at=now() WHERE workspace_id=$1 AND id=$2`, s.workspaceID, runID, status, httpStatus, nilIfEmptyJSON(output), code, message); err != nil {
			return err
		}
		if record.ID == "" {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='completed',http_status=$3,response=$4 WHERE workspace_id=$1 AND id=$2 AND status='running' AND operation_id=$5`, s.workspaceID, record.ID, httpStatus, response, runID)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return err
	})
}
