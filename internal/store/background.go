package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

type WebhookDeliveryWork struct {
	DeliveryID  string
	EndpointID  string
	EventID     string
	EventType   string
	Payload     json.RawMessage
	TargetURL   string
	FallbackURL string
	SecretBlob  []byte
	Attempt     int
	Status      string
}

func (s *Store) MarkOperationRunning(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE operation_runs SET status = 'running' WHERE workspace_id = $1 AND id = $2 AND status = 'queued'`, s.workspaceID, id)
	return err
}

func (s *Store) EnqueueDueSyncTasks(ctx context.Context, limit int) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT st.id::text FROM sync_tasks st WHERE st.workspace_id=$1 AND st.status='deployed' AND st.schedule_type='interval' AND st.deleted_at IS NULL AND st.next_run_at<=now()
 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.kind='sync_run' AND j.resource_id=st.id AND j.status IN ('queued','running'))
 ORDER BY st.next_run_at,st.id FOR UPDATE OF st SKIP LOCKED LIMIT $2`, s.workspaceID, limit)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			task, err := s.SyncTask(ctx, id)
			if err != nil {
				return err
			}
			requestID := "schedule-" + id + "-" + task.NextRunAt.UTC().Format(time.RFC3339Nano)
			if _, err := s.enqueueSync(ctx, tx, task, "schedule", requestID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE sync_tasks SET next_run_at=now()+CASE cron_expression WHEN '每 15 分钟' THEN interval '15 minutes' WHEN '每 30 分钟' THEN interval '30 minutes' WHEN '每小时' THEN interval '1 hour' WHEN '每天' THEN interval '1 day' END WHERE workspace_id=$1 AND id=$2`, s.workspaceID, id); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func (s *Store) ExpandOutbox(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text, event_type, payload FROM outbox_events WHERE workspace_id = $1 AND status = 'pending' AND available_at <= now() ORDER BY available_at, id FOR UPDATE SKIP LOCKED LIMIT $2`, s.workspaceID, limit)
		if err != nil {
			return err
		}
		type event struct {
			id, kind string
			payload  []byte
		}
		events := []event{}
		for rows.Next() {
			var item event
			if err := rows.Scan(&item.id, &item.kind, &item.payload); err != nil {
				rows.Close()
				return err
			}
			events = append(events, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, item := range events {
			endpointRows, err := tx.Query(ctx, `
				SELECT DISTINCT we.id::text, we.primary_url FROM webhook_endpoints we
				JOIN webhook_endpoint_events wee ON wee.webhook_endpoint_id = we.id AND wee.workspace_id = we.workspace_id
				WHERE we.workspace_id = $1 AND we.status = 'active' AND we.deleted_at IS NULL
				  AND (wee.event_pattern = '*' OR wee.event_pattern = $2 OR (right(wee.event_pattern, 2) = '.*' AND $2 LIKE left(wee.event_pattern, length(wee.event_pattern)-1) || '%'))`, s.workspaceID, item.kind)
			if err != nil {
				return err
			}
			type endpoint struct {
				id     string
				target string
			}
			endpoints := make([]endpoint, 0)
			for endpointRows.Next() {
				var current endpoint
				if err := endpointRows.Scan(&current.id, &current.target); err != nil {
					endpointRows.Close()
					return err
				}
				endpoints = append(endpoints, current)
			}
			if err := endpointRows.Err(); err != nil {
				endpointRows.Close()
				return err
			}
			endpointRows.Close()
			for _, current := range endpoints {
				deliveryID := StableID("webhook-delivery", item.id+":"+current.id)
				command, err := tx.Exec(ctx, `INSERT INTO webhook_deliveries(id, workspace_id, outbox_event_id, webhook_endpoint_id, target_url, status, next_attempt_at) VALUES($1,$2,$3,$4,$5,'pending',now()) ON CONFLICT(workspace_id,outbox_event_id,webhook_endpoint_id) DO NOTHING`, deliveryID, s.workspaceID, item.id, current.id, current.target)
				if err != nil {
					return err
				}
				if command.RowsAffected() > 0 {
					jobID := StableID("webhook-job", deliveryID)
					if _, err := tx.Exec(ctx, `INSERT INTO jobs(id,workspace_id,kind,resource_id,payload,status,run_after) VALUES($1,$2,'webhook_delivery',$3,'{}','queued',now()) ON CONFLICT(id) DO NOTHING`, jobID, s.workspaceID, deliveryID); err != nil {
						return err
					}
					count++
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='published', published_at=now() WHERE workspace_id=$1 AND id=$2`, s.workspaceID, item.id); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

func (s *Store) WebhookDeliveryWork(ctx context.Context, id string) (WebhookDeliveryWork, error) {
	var item WebhookDeliveryWork
	err := s.pool.QueryRow(ctx, `
		SELECT wd.id::text, we.id::text, oe.id::text, oe.event_type, oe.payload,
		       wd.target_url, COALESCE(we.fallback_url, ''), we.secret_blob, wd.attempt_count,wd.status
		FROM webhook_deliveries wd JOIN outbox_events oe ON oe.id=wd.outbox_event_id AND oe.workspace_id=wd.workspace_id
		JOIN webhook_endpoints we ON we.id=wd.webhook_endpoint_id AND we.workspace_id=wd.workspace_id
		WHERE wd.workspace_id=$1 AND wd.id=$2`, s.workspaceID, id).Scan(&item.DeliveryID, &item.EndpointID, &item.EventID, &item.EventType, &item.Payload, &item.TargetURL, &item.FallbackURL, &item.SecretBlob, &item.Attempt, &item.Status)
	return item, mapNotFound(err)
}

func (s *Store) FinishWebhookDeliveryAttempt(ctx context.Context, job model.Job, attempt model.WebhookDeliveryAttempt, delivered, dead bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.fenceJob(ctx, tx, job); err != nil {
			return err
		}
		if attempt.ID == "" {
			attempt.ID = StableID("webhook-attempt", attempt.WebhookDeliveryID+":"+strconv.Itoa(attempt.AttemptNo))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO webhook_delivery_attempts(
				id, workspace_id, webhook_delivery_id, attempt_no, target_url,
				request_id, http_status, duration_ms, error_message
			) VALUES($1, $2, $3, $4, $5, $6, NULLIF($7, 0), $8, NULLIF($9, ''))
			ON CONFLICT(webhook_delivery_id, attempt_no) DO NOTHING`,
			attempt.ID, s.workspaceID, attempt.WebhookDeliveryID, attempt.AttemptNo,
			attempt.TargetURL, attempt.RequestID, attempt.HTTPStatus,
			attempt.DurationMS, attempt.ErrorMessage); err != nil {
			return err
		}
		if delivered {
			_, err := tx.Exec(ctx, `
				UPDATE webhook_deliveries SET status='delivered', attempt_count=$3,
				       last_http_status=$4, last_error=NULL, next_attempt_at=NULL,
				       delivered_at=now(), dead_at=NULL
				WHERE workspace_id=$1 AND id=$2`, s.workspaceID,
				attempt.WebhookDeliveryID, attempt.AttemptNo, attempt.HTTPStatus)
			return err
		}
		state := "retrying"
		var next any = time.Now().UTC().Add(30 * time.Second)
		var deadAt any
		if dead {
			state = "dead"
			next = nil
			deadAt = time.Now().UTC()
		}
		_, err := tx.Exec(ctx, `
			UPDATE webhook_deliveries SET status=$3, attempt_count=$4,
			       last_http_status=NULLIF($5,0), last_error=$6,
			       next_attempt_at=$7, dead_at=$8
			WHERE workspace_id=$1 AND id=$2`, s.workspaceID,
			attempt.WebhookDeliveryID, state, attempt.AttemptNo,
			attempt.HTTPStatus, attempt.ErrorMessage, next, deadAt)
		return err
	})
}
