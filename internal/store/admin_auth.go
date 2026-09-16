package store

import (
	"context"
	"time"
)

type AdminUser struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string
	Role         string
	AuthVersion  int64
}

type AdminSession struct {
	ID          string
	UserID      string
	Email       string
	DisplayName string
	Role        string
	ExpiresAt   time.Time
}

func (s *Store) AdminUserByEmail(ctx context.Context, email string) (AdminUser, error) {
	var item AdminUser
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.display_name, COALESCE(u.password_hash, ''),
		       wm.role, u.auth_version
		FROM users u
		JOIN workspace_members wm ON wm.user_id = u.id AND wm.workspace_id = $1
		WHERE lower(u.email) = lower($2) AND u.status = 'active' AND wm.status = 'active'`,
		s.workspaceID, email).Scan(&item.ID, &item.Email, &item.DisplayName, &item.PasswordHash, &item.Role, &item.AuthVersion)
	return item, mapNotFound(err)
}

func (s *Store) CreateAdminSession(ctx context.Context, id, userID, tokenHash, ipAddress, userAgent string, authVersion int64, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_sessions(
			id, user_id, workspace_id, token_hash, auth_version,
			ip_address, user_agent, expires_at, last_used_at
		) VALUES($1, $2, $3, $4, $5, NULLIF($6, '')::inet, NULLIF($7, ''), $8, now())`,
		id, userID, s.workspaceID, tokenHash, authVersion, ipAddress, userAgent, expiresAt)
	return err
}

func (s *Store) AdminSessionByTokenHash(ctx context.Context, tokenHash string) (AdminSession, error) {
	var item AdminSession
	err := s.pool.QueryRow(ctx, `
		SELECT us.id::text, u.id::text, u.email, u.display_name, wm.role, us.expires_at
		FROM user_sessions us
		JOIN users u ON u.id = us.user_id
		JOIN workspace_members wm ON wm.user_id = u.id AND wm.workspace_id = us.workspace_id
		WHERE us.workspace_id = $1 AND us.token_hash = $2
		  AND us.revoked_at IS NULL AND us.expires_at > now()
		  AND us.auth_version = u.auth_version
		  AND u.status = 'active' AND wm.status = 'active'`,
		s.workspaceID, tokenHash).Scan(&item.ID, &item.UserID, &item.Email, &item.DisplayName, &item.Role, &item.ExpiresAt)
	if err != nil {
		return AdminSession{}, mapNotFound(err)
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE user_sessions SET last_used_at = now()
		WHERE workspace_id = $1 AND id = $2
		  AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, s.workspaceID, item.ID)
	return item, nil
}

func (s *Store) RevokeAdminSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now()
		WHERE workspace_id = $1 AND token_hash = $2 AND revoked_at IS NULL`, s.workspaceID, tokenHash)
	return err
}
