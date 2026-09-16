package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListTeamMembers(ctx context.Context) ([]model.TeamMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT wm.id::text, wm.user_id::text, u.email, u.display_name,
		       wm.role, wm.status, wm.version, wm.created_at, wm.updated_at
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1
		ORDER BY CASE wm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'developer' THEN 2 ELSE 3 END,
		         u.display_name, wm.id`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TeamMember, 0)
	for rows.Next() {
		var item model.TeamMember
		if err := rows.Scan(&item.ID, &item.UserID, &item.Email, &item.DisplayName, &item.Role,
			&item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) InviteMember(ctx context.Context, email, role, invitedBy, tokenHash string, expiresAt time.Time) (model.TeamMember, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	userID := StableID("user", email)
	memberID := StableID("member", s.workspaceID+":"+userID)
	var existingID, existingStatus string
	err := s.pool.QueryRow(ctx, `SELECT id::text,status FROM users WHERE lower(email)=lower($1)`, email).Scan(&existingID, &existingStatus)
	if err == nil {
		if existingStatus != "invited" {
			return model.TeamMember{}, fmt.Errorf("%w: 该邮箱已有账号，请联系管理员管理其现有成员身份", ErrConflict)
		}
		userID = existingID
		memberID = StableID("member", s.workspaceID+":"+userID)
	} else if err != pgx.ErrNoRows {
		return model.TeamMember{}, err
	}
	invitationID := StableID("invitation", s.workspaceID+":"+email+":"+time.Now().UTC().Format(time.RFC3339Nano))
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO users(id, email, display_name, status)
			VALUES($1, $2, $3, 'invited')
			ON CONFLICT(id) DO UPDATE SET email = EXCLUDED.email, updated_at = now()`,
			userID, email, strings.Split(email, "@")[0]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workspace_members(id, workspace_id, user_id, role, status)
			VALUES($1, $2, $3, $4, 'invited')
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET
				role = EXCLUDED.role, status = 'invited', version = workspace_members.version + 1, updated_at = now() WHERE workspace_members.status='invited'`,
			memberID, s.workspaceID, userID, role); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workspace_invitations SET revoked_at=now() WHERE workspace_id=$1 AND lower(email)=lower($2) AND accepted_at IS NULL AND revoked_at IS NULL`, s.workspaceID, email); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
   INSERT INTO workspace_invitations(
				id, workspace_id, email, role, token_hash, invited_by, expires_at
			) VALUES($1, $2, $3, $4, $5, $6, $7)`, invitationID, s.workspaceID,
			email, role, tokenHash, invitedBy, expiresAt)
		return err
	})
	if err != nil {
		return model.TeamMember{}, err
	}
	return s.TeamMember(ctx, memberID)
}

func (s *Store) TeamMember(ctx context.Context, id string) (model.TeamMember, error) {
	var item model.TeamMember
	err := s.pool.QueryRow(ctx, `
		SELECT wm.id::text, wm.user_id::text, u.email, u.display_name,
		       wm.role, wm.status, wm.version, wm.created_at, wm.updated_at
		FROM workspace_members wm JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1 AND wm.id = $2`, s.workspaceID, id).Scan(
		&item.ID, &item.UserID, &item.Email, &item.DisplayName, &item.Role,
		&item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) UpdateMemberRole(ctx context.Context, id, role string, version int64) (model.TeamMember, error) {
	returnItem := model.TeamMember{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.workspaceID+":owners"); err != nil {
			return err
		}
		var currentRole string
		if err := tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id).Scan(&currentRole); err != nil {
			return mapNotFound(err)
		}
		if currentRole == "owner" && role != "owner" {
			var owners int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner' AND status = 'active'`, s.workspaceID).Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return fmt.Errorf("%w: workspace must retain an owner", ErrConflict)
			}
		}
		command, err := tx.Exec(ctx, `
			UPDATE workspace_members SET role = $4, version = version + 1, updated_at = now()
			WHERE workspace_id = $1 AND id = $2 AND version = $3`, s.workspaceID, id, version, role)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return returnItem, err
	}
	return s.TeamMember(ctx, id)
}

func (s *Store) RemoveMember(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var role, userID string
		if err := tx.QueryRow(ctx, `SELECT role, user_id::text FROM workspace_members WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id).Scan(&role, &userID); err != nil {
			return mapNotFound(err)
		}
		if role == "owner" {
			return fmt.Errorf("%w: owner cannot be removed", ErrConflict)
		}
		if _, err := tx.Exec(ctx, `UPDATE workspace_invitations SET revoked_at=now() WHERE workspace_id=$1 AND email=(SELECT email FROM users WHERE id=$2) AND accepted_at IS NULL AND revoked_at IS NULL`, s.workspaceID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE workspace_id = $1 AND user_id = $2 AND revoked_at IS NULL`, s.workspaceID, userID)
		return nil
	})
}

func (s *Store) Settings(ctx context.Context) (model.PlatformSettings, error) {
	var item model.PlatformSettings
	err := s.pool.QueryRow(ctx, `
		SELECT workspace_id::text, platform_name, public_base_url, runtime_parameters,
		       operation_retention_days, audit_retention_days, version, updated_at
		FROM platform_settings WHERE workspace_id = $1`, s.workspaceID).Scan(
		&item.WorkspaceID, &item.PlatformName, &item.PublicBaseURL, &item.RuntimeParameters,
		&item.OperationRetentionDays, &item.AuditRetentionDays, &item.Version, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) SaveSettings(ctx context.Context, item model.PlatformSettings, updatedBy string) (model.PlatformSettings, error) {
	command, err := s.pool.Exec(ctx, `
		UPDATE platform_settings SET platform_name = $3, public_base_url = $4,
		       runtime_parameters = $5, operation_retention_days = $6,
		       audit_retention_days = $7, version = version + 1,
		       updated_by = $8, updated_at = now()
		WHERE workspace_id = $1 AND version = $2`, s.workspaceID, item.Version,
		item.PlatformName, item.PublicBaseURL, jsonOrObject(item.RuntimeParameters),
		item.OperationRetentionDays, item.AuditRetentionDays, updatedBy)
	if err != nil {
		return model.PlatformSettings{}, err
	}
	if command.RowsAffected() == 0 {
		return model.PlatformSettings{}, ErrConflict
	}
	return s.Settings(ctx)
}

func (s *Store) OperationExpiresAt(ctx context.Context, startedAt time.Time) (time.Time, error) {
	var days int
	if err := s.pool.QueryRow(ctx, `SELECT operation_retention_days FROM platform_settings WHERE workspace_id = $1`, s.workspaceID).Scan(&days); err != nil {
		return time.Time{}, mapNotFound(err)
	}
	return startedAt.Add(time.Duration(days) * 24 * time.Hour), nil
}

func (s *Store) WriteAudit(ctx context.Context, item model.AuditLog) error {
	expiresAt, err := s.auditExpiresAt(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_logs(
			id, workspace_id, request_id, actor_type, actor_id, actor_label,
			action, resource_type, resource_id, resource_label, ip_address,
			before_data, after_data, metadata, expires_at
		) VALUES($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, $8,
		         NULLIF($9, '')::uuid, NULLIF($10, ''), NULLIF($11, '')::inet,
		         $12, $13, $14, $15)`,
		item.ID, s.workspaceID, item.RequestID, item.ActorType, item.ActorID,
		item.ActorLabel, item.Action, item.ResourceType, item.ResourceID,
		item.ResourceLabel, item.IPAddress, nilIfEmptyJSON(item.BeforeData),
		nilIfEmptyJSON(item.AfterData), jsonOrObject(item.Metadata), expiresAt)
	return err
}

func (s *Store) auditExpiresAt(ctx context.Context, createdAt time.Time) (any, error) {
	var days *int
	if err := s.pool.QueryRow(ctx, `SELECT audit_retention_days FROM platform_settings WHERE workspace_id = $1`, s.workspaceID).Scan(&days); err != nil {
		return nil, mapNotFound(err)
	}
	if days == nil {
		return nil, nil
	}
	return createdAt.Add(time.Duration(*days) * 24 * time.Hour), nil
}

func (s *Store) CleanupExpired(ctx context.Context) (int64, error) {
	var removed int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, query := range []string{
			`UPDATE idempotency_records SET status='unknown' WHERE workspace_id=$1 AND id IN(SELECT id FROM idempotency_records WHERE workspace_id=$1 AND status='running' AND created_at<now()-interval '5 minutes' LIMIT 1000)`,
			`UPDATE operation_runs SET status='unknown',completed_at=now(),error_code='interrupted',error_message='Worker or request ended without a durable result' WHERE workspace_id=$1 AND kind='action' AND status='running' AND started_at<now()-interval '5 minutes'`,
			`UPDATE jobs SET status='dead',completed_at=now(),lease_owner=NULL,lease_expires_at=NULL,last_error='lease expired after final attempt' WHERE workspace_id=$1 AND status='running' AND attempt>=max_attempts AND lease_expires_at<now()`,
			`UPDATE operation_runs o SET status='failed',completed_at=now(),error_code='job_exhausted' WHERE o.workspace_id=$1 AND o.status IN ('queued','running') AND EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.operation_id=o.id AND j.status='dead') AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.operation_id=o.id AND j.status IN ('queued','running'))`,
			`UPDATE webhook_deliveries d SET status='dead',dead_at=now(),next_attempt_at=NULL WHERE d.workspace_id=$1 AND d.status IN ('pending','retrying') AND EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.resource_id=d.id AND j.kind='webhook_delivery' AND j.status='dead')`,
			`DELETE FROM webhook_delivery_attempts WHERE workspace_id=$1 AND id IN (SELECT a.id FROM webhook_delivery_attempts a JOIN webhook_deliveries d ON d.id=a.webhook_delivery_id WHERE a.workspace_id=$1 AND d.status IN ('delivered','dead') AND d.created_at<now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.resource_id=d.id AND j.status IN ('queued','running')) LIMIT 1000)`,
			`DELETE FROM webhook_deliveries WHERE workspace_id=$1 AND id IN (SELECT d.id FROM webhook_deliveries d WHERE d.workspace_id=$1 AND d.status IN ('delivered','dead') AND d.created_at<now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM webhook_delivery_attempts a WHERE a.webhook_delivery_id=d.id) AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=$1 AND j.resource_id=d.id AND j.status IN ('queued','running')) LIMIT 1000)`,
			`DELETE FROM outbox_events WHERE workspace_id=$1 AND id IN (SELECT e.id FROM outbox_events e WHERE e.workspace_id=$1 AND e.status='published' AND e.published_at<now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM webhook_deliveries d WHERE d.outbox_event_id=e.id) LIMIT 1000)`,
			`DELETE FROM webhook_ingress_events WHERE workspace_id=$1 AND id IN(SELECT id FROM webhook_ingress_events WHERE workspace_id=$1 AND expires_at<=now() LIMIT 1000)`,
			`DELETE FROM user_sessions WHERE workspace_id=$1 AND id IN(SELECT id FROM user_sessions WHERE workspace_id=$1 AND (expires_at<=now() OR revoked_at<now()-interval '1 day') LIMIT 1000)`,
			`DELETE FROM workspace_invitations WHERE workspace_id=$1 AND id IN(SELECT id FROM workspace_invitations WHERE workspace_id=$1 AND expires_at<=now() LIMIT 1000)`,
			`DELETE FROM idempotency_records WHERE workspace_id=$1 AND id IN(SELECT id FROM idempotency_records WHERE workspace_id=$1 AND ((status='completed' AND expires_at<=now()) OR (status='claimed' AND created_at<now()-interval '5 minutes')) LIMIT 1000)`,
			`DELETE FROM jobs WHERE workspace_id=$1 AND id IN(SELECT id FROM jobs WHERE workspace_id=$1 AND status IN ('succeeded','dead') AND completed_at<now()-interval '30 days' LIMIT 1000)`,
			`DELETE FROM operation_events WHERE workspace_id=$1 AND id IN(SELECT e.id FROM operation_events e JOIN operation_runs o ON o.id=e.operation_id WHERE e.workspace_id=$1 AND o.expires_at<=now() AND o.status IN ('success','failed') AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.operation_id=o.id AND j.status IN ('queued','running')) LIMIT 1000)`,
			`UPDATE sync_task_states SET last_operation_id=NULL WHERE workspace_id=$1 AND last_operation_id IN(SELECT id FROM operation_runs WHERE workspace_id=$1 AND expires_at<=now() AND status IN ('success','failed'))`,
			`DELETE FROM operation_runs WHERE workspace_id=$1 AND id IN(SELECT o.id FROM operation_runs o WHERE o.workspace_id=$1 AND o.expires_at<=now() AND o.status IN ('success','failed') AND NOT EXISTS(SELECT 1 FROM operation_events e WHERE e.operation_id=o.id) AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.operation_id=o.id) AND NOT EXISTS(SELECT 1 FROM idempotency_records i WHERE i.operation_id=o.id) LIMIT 1000)`,
			`DELETE FROM audit_logs WHERE workspace_id=$1 AND id IN(SELECT id FROM audit_logs WHERE workspace_id=$1 AND expires_at<=now() LIMIT 1000)`,
		} {
			tag, err := tx.Exec(ctx, query, s.workspaceID)
			if err != nil {
				return err
			}
			removed += tag.RowsAffected()
		}
		return nil
	})
	return removed, err
}

func (s *Store) ListAudit(ctx context.Context, query string, limit int, options ...ListOptions) ([]model.AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, request_id, actor_type, COALESCE(actor_id::text, ''), actor_label,
		       action, resource_type, COALESCE(resource_id::text, ''), COALESCE(resource_label, ''),
		       COALESCE(ip_address::text, ''), before_data, after_data, metadata, created_at
		FROM audit_logs
		WHERE workspace_id = $1
		  AND ($2 = '' OR actor_label ILIKE '%' || $2 || '%' OR action ILIKE '%' || $2 || '%'
		       OR resource_label ILIKE '%' || $2 || '%')
		AND ($4='' OR (created_at,id)<($5,NULLIF($4,'')::uuid))
 AND ($6::timestamptz IS NULL OR created_at >= $6) AND ($7::timestamptz IS NULL OR created_at <= $7)
 ORDER BY created_at DESC, id DESC LIMIT $3`, s.workspaceID, strings.TrimSpace(query), limit, o.ID, o.Before, nullableTime(o.From), nullableTime(o.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuditLog, 0)
	for rows.Next() {
		var item model.AuditLog
		if err := rows.Scan(
			&item.ID, &item.RequestID, &item.ActorType, &item.ActorID, &item.ActorLabel,
			&item.Action, &item.ResourceType, &item.ResourceID, &item.ResourceLabel,
			&item.IPAddress, &item.BeforeData, &item.AfterData, &item.Metadata, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func MarshalAuditValue(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}
