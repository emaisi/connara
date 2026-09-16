package store

import (
	"apihub-go/internal/model"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// AcceptInvitation consumes a workspace-scoped token once. Passwords are only
// set for newly invited users; existing accounts must never be reset by an invitation.
func (s *Store) AcceptInvitation(ctx context.Context, hash, passwordHash, displayName string) (model.TeamMember, error) {
	var memberID string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id, email, role string
		if err := tx.QueryRow(ctx, `SELECT id::text,email,role FROM workspace_invitations WHERE workspace_id=$1 AND token_hash=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now() FOR UPDATE`, s.workspaceID, hash).Scan(&id, &email, &role); err != nil {
			return mapNotFound(err)
		}
		var userID, status string
		if err := tx.QueryRow(ctx, `SELECT id::text,status FROM users WHERE lower(email)=lower($1) FOR UPDATE`, email).Scan(&userID, &status); err != nil {
			return mapNotFound(err)
		}
		if status != "invited" {
			return fmt.Errorf("%w: 请使用已有账号登录后联系管理员添加成员，不可通过邀请重设密码", ErrConflict)
		}
		tag, err := tx.Exec(ctx, `UPDATE workspace_members SET status='active',role=$3,version=version+1,updated_at=now() WHERE workspace_id=$1 AND user_id=$2 AND status='invited'`, s.workspaceID, userID, role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2,display_name=$3,status='active',auth_version=auth_version+1,updated_at=now() WHERE id=$1`, userID, passwordHash, displayName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workspace_invitations SET accepted_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workspace_invitations SET revoked_at=now() WHERE workspace_id=$1 AND lower(email)=lower($2) AND id<>$3 AND accepted_at IS NULL AND revoked_at IS NULL`, s.workspaceID, email, id); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id::text FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, s.workspaceID, userID).Scan(&memberID)
	})
	if err != nil {
		return model.TeamMember{}, err
	}
	return s.TeamMember(ctx, memberID)
}
