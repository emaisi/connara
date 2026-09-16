package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListIntegrations(ctx context.Context) ([]model.Integration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id::text, i.workspace_id::text, i.integration_key, i.name,
		       i.system_id::text, s.system_key, s.name,
		       i.auth_instance_id::text, ai.name, i.base_url, i.status,
		       i.settings, i.version, i.created_at, i.updated_at
		FROM integrations i
		JOIN systems s ON s.id = i.system_id AND s.workspace_id = i.workspace_id
		JOIN auth_instances ai ON ai.id = i.auth_instance_id AND ai.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.deleted_at IS NULL
		ORDER BY i.updated_at DESC, i.id DESC`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Integration, 0)
	for rows.Next() {
		item, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Integration(ctx context.Context, idOrKey string) (model.Integration, error) {
	item, err := scanIntegration(s.pool.QueryRow(ctx, `
		SELECT i.id::text, i.workspace_id::text, i.integration_key, i.name,
		       i.system_id::text, s.system_key, s.name,
		       i.auth_instance_id::text, ai.name, i.base_url, i.status,
		       i.settings, i.version, i.created_at, i.updated_at
		FROM integrations i
		JOIN systems s ON s.id = i.system_id AND s.workspace_id = i.workspace_id
		JOIN auth_instances ai ON ai.id = i.auth_instance_id AND ai.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.deleted_at IS NULL
		  AND (i.id::text = $2 OR i.integration_key = $2)`, s.workspaceID, idOrKey))
	return item, mapNotFound(err)
}

func (s *Store) IntegrationForSystem(ctx context.Context, systemKey, idOrKey string) (model.Integration, error) {
	query := `
		SELECT i.id::text, i.workspace_id::text, i.integration_key, i.name,
		       i.system_id::text, s.system_key, s.name,
		       i.auth_instance_id::text, ai.name, i.base_url, i.status,
		       i.settings, i.version, i.created_at, i.updated_at
		FROM integrations i
		JOIN systems s ON s.id = i.system_id AND s.workspace_id = i.workspace_id
		JOIN auth_instances ai ON ai.id = i.auth_instance_id AND ai.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.deleted_at IS NULL AND i.status = 'ready'
		  AND s.system_key = $2`
	args := []any{s.workspaceID, systemKey}
	if idOrKey != "" {
		query += ` AND (i.id::text = $3 OR i.integration_key = $3)`
		args = append(args, idOrKey)
	}
	query += ` ORDER BY i.created_at, i.id LIMIT 1`
	item, err := scanIntegration(s.pool.QueryRow(ctx, query, args...))
	return item, mapNotFound(err)
}

func scanIntegration(row rowScanner) (model.Integration, error) {
	var item model.Integration
	err := row.Scan(
		&item.ID, &item.WorkspaceID, &item.IntegrationKey, &item.Name,
		&item.SystemID, &item.SystemKey, &item.SystemName,
		&item.AuthInstanceID, &item.AuthName, &item.BaseURL, &item.Status,
		&item.Settings, &item.Version, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) SaveIntegration(ctx context.Context, item model.Integration) (model.Integration, error) {
	if item.ID == "" {
		item.ID = StableID("integration", s.workspaceID+":"+item.IntegrationKey)
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {

		var oldAuth, oldSystem, oldBase string
		loadErr := tx.QueryRow(ctx, `SELECT auth_instance_id::text,system_id::text,base_url FROM integrations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, s.workspaceID, item.ID).Scan(&oldAuth, &oldSystem, &oldBase)
		if loadErr != nil && !errors.Is(loadErr, pgx.ErrNoRows) {
			return loadErr
		}
		if loadErr == nil && (oldAuth != item.AuthInstanceID || oldSystem != item.SystemID || oldBase != strings.TrimRight(item.BaseURL, "/")) {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connections WHERE workspace_id=$1 AND integration_id=$2 AND deleted_at IS NULL)`, s.workspaceID, item.ID).Scan(&used); err != nil {
				return err
			}
			if used {
				return fmt.Errorf("%w: create a new integration to change the target or authentication of existing connections", ErrConflict)
			}
		}
		var authSystemID, authStatus string
		if err := tx.QueryRow(ctx, `
			SELECT system_id::text, status FROM auth_instances
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL FOR SHARE`,
			s.workspaceID, item.AuthInstanceID).Scan(&authSystemID, &authStatus); err != nil {
			return mapNotFound(err)
		}
		if authSystemID != item.SystemID {
			return fmt.Errorf("%w: auth instance belongs to another system", ErrConflict)
		}
		if item.Status == "ready" && authStatus != "ready" {
			return fmt.Errorf("%w: auth instance is not ready", ErrConflict)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO integrations(
				id, workspace_id, integration_key, name, system_id, auth_instance_id,
				base_url, status, settings
			) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT(id) DO UPDATE SET
				integration_key = EXCLUDED.integration_key, name = EXCLUDED.name,
				system_id = EXCLUDED.system_id, auth_instance_id = EXCLUDED.auth_instance_id,
				base_url = EXCLUDED.base_url, status = EXCLUDED.status,
				settings = EXCLUDED.settings, version = integrations.version + 1, updated_at = now()
			WHERE integrations.workspace_id = EXCLUDED.workspace_id`,
			item.ID, s.workspaceID, item.IntegrationKey, item.Name, item.SystemID,
			item.AuthInstanceID, strings.TrimRight(item.BaseURL, "/"), item.Status, jsonOrObject(item.Settings))
		return err
	})
	if err != nil {
		return model.Integration{}, err
	}
	return s.Integration(ctx, item.ID)
}

func (s *Store) ListConnections(ctx context.Context, options ...ListOptions) ([]model.Connection, error) {
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, connectionSelect+`
		WHERE c.workspace_id = $1 AND c.deleted_at IS NULL
		AND ($2='' OR (c.updated_at,c.id)<($3,NULLIF($2,'')::uuid))
 AND ($4='' OR c.connection_key ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%' OR eu.external_key ILIKE '%'||$4||'%')
 AND ($5='' OR c.status=$5)
 ORDER BY c.updated_at DESC, c.id DESC LIMIT $6`, s.workspaceID, o.ID, o.Before, o.Query, o.Status, o.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Connection, 0)
	for rows.Next() {
		item, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const connectionSelect = `
	SELECT c.id::text, c.workspace_id::text, c.connection_key, c.name,
	       c.integration_id::text, i.integration_key, s.system_key,
	       c.auth_instance_id::text, c.end_user_id::text, eu.external_key, COALESCE(eu.display_name,''), COALESCE(eu.email,''), eu.metadata,
	       c.status, c.credential_blob, c.key_version, c.revision,
	       c.token_expires_at, c.last_verified_at, c.last_used_at,
	       COALESCE(c.last_error_code, ''), COALESCE(c.last_error_message, ''),
	       c.tags, c.created_at, c.updated_at
	FROM connections c
	JOIN integrations i ON i.id = c.integration_id AND i.workspace_id = c.workspace_id
	JOIN systems s ON s.id = i.system_id AND s.workspace_id = c.workspace_id
	JOIN end_users eu ON eu.id = c.end_user_id AND eu.workspace_id = c.workspace_id`

func (s *Store) Connection(ctx context.Context, idOrKey string) (model.Connection, error) {
	item, err := scanConnection(s.pool.QueryRow(ctx, connectionSelect+`
		WHERE c.workspace_id = $1 AND c.deleted_at IS NULL
		  AND (c.id::text = $2 OR c.connection_key = $2)`, s.workspaceID, idOrKey))
	return item, mapNotFound(err)
}

func (s *Store) ConnectionForIntegration(ctx context.Context, integrationID, idOrKey string) (model.Connection, error) {
	query := connectionSelect + `
		WHERE c.workspace_id = $1 AND c.integration_id = $2 AND c.deleted_at IS NULL AND c.status = 'active'`
	args := []any{s.workspaceID, integrationID}
	if idOrKey != "" {
		query += ` AND (c.id::text = $3 OR c.connection_key = $3 OR c.name = $3)`
		args = append(args, idOrKey)
	}
	query += ` ORDER BY c.created_at, c.id LIMIT 1`
	item, err := scanConnection(s.pool.QueryRow(ctx, query, args...))
	return item, mapNotFound(err)
}

func scanConnection(row rowScanner) (model.Connection, error) {
	var item model.Connection
	err := row.Scan(
		&item.ID, &item.WorkspaceID, &item.ConnectionKey, &item.Name,
		&item.IntegrationID, &item.IntegrationKey, &item.SystemKey,
		&item.AuthInstanceID, &item.EndUserID, &item.EndUserKey, &item.EndUserName, &item.EndUserEmail, &item.Metadata,
		&item.Status, &item.CredentialBlob, &item.KeyVersion, &item.Revision,
		&item.TokenExpiresAt, &item.LastVerifiedAt, &item.LastUsedAt,
		&item.LastErrorCode, &item.LastErrorMessage, &item.Tags,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

type SaveConnectionInput struct {
	Connection       model.Connection
	EndUser          model.EndUser
	ExpectedRevision int64
}

func (s *Store) SaveConnection(ctx context.Context, input SaveConnectionInput) (model.Connection, error) {
	connection := input.Connection
	endUser := input.EndUser
	if connection.ID == "" {
		connection.ID = StableID("connection", s.workspaceID+":"+connection.ConnectionKey)
	}
	if endUser.ID == "" {
		endUser.ID = StableID("end-user", s.workspaceID+":"+endUser.ExternalKey)
	}
	if connection.Status == "" {
		connection.Status = "active"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var integrationAuthID string
		if err := tx.QueryRow(ctx, `
			SELECT auth_instance_id::text FROM integrations
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL FOR SHARE`,
			s.workspaceID, connection.IntegrationID).Scan(&integrationAuthID); err != nil {
			return mapNotFound(err)
		}
		if integrationAuthID != connection.AuthInstanceID {
			return fmt.Errorf("%w: connection auth instance differs from integration", ErrConflict)
		}
		var storedRevision int64
		err := tx.QueryRow(ctx, `
			SELECT revision FROM connections
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
			FOR UPDATE`, s.workspaceID, connection.ID).Scan(&storedRevision)
		switch {
		case err == nil && input.ExpectedRevision != storedRevision:
			return fmt.Errorf("%w: connection revision changed", ErrConflict)
		case errors.Is(err, pgx.ErrNoRows) && input.ExpectedRevision != 0:
			return ErrConflict
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO end_users(id, workspace_id, external_key, display_name, email, metadata)
			VALUES($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
			ON CONFLICT(workspace_id, external_key) DO UPDATE SET
				display_name = EXCLUDED.display_name, email = EXCLUDED.email,
				metadata = EXCLUDED.metadata, updated_at = now()`,
			endUser.ID, s.workspaceID, endUser.ExternalKey, endUser.DisplayName,
			endUser.Email, jsonOrObject(endUser.Metadata)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id::text FROM end_users WHERE workspace_id = $1 AND external_key = $2`,
			s.workspaceID, endUser.ExternalKey).Scan(&endUser.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO connections(
				id, workspace_id, connection_key, name, integration_id, auth_instance_id,
				end_user_id, status, credential_blob, key_version, revision, token_expires_at,
				last_verified_at, tags
			) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, $11, $12, $13)
			ON CONFLICT(id) DO UPDATE SET
				connection_key = EXCLUDED.connection_key, name = EXCLUDED.name,
				integration_id = EXCLUDED.integration_id, auth_instance_id = EXCLUDED.auth_instance_id,
				end_user_id = EXCLUDED.end_user_id, status = EXCLUDED.status,
				credential_blob = EXCLUDED.credential_blob, key_version = EXCLUDED.key_version,
				revision = connections.revision + 1, token_expires_at = EXCLUDED.token_expires_at,
				last_verified_at = EXCLUDED.last_verified_at, tags = EXCLUDED.tags,
				last_error_code = NULL, last_error_message = NULL, updated_at = now()
			WHERE connections.workspace_id = EXCLUDED.workspace_id`,
			connection.ID, s.workspaceID, connection.ConnectionKey, connection.Name,
			connection.IntegrationID, connection.AuthInstanceID, endUser.ID, connection.Status,
			connection.CredentialBlob, connection.KeyVersion, nullTime(connection.TokenExpiresAt),
			nullTime(connection.LastVerifiedAt), connection.Tags)
		return err
	})
	if err != nil {
		return model.Connection{}, err
	}
	return s.Connection(ctx, connection.ID)
}

func (s *Store) UpdateConnectionCredential(ctx context.Context, id string, expectedRevision int64, blob []byte, expiresAt *time.Time, keyVersion int16) (model.Connection, error) {
	command, err := s.pool.Exec(ctx, `
		UPDATE connections SET credential_blob = $4, token_expires_at = $5, key_version = $6,
		       revision = revision + 1, status = 'active', last_verified_at = now(),
		       last_error_code = NULL, last_error_message = NULL, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND revision = $3 AND deleted_at IS NULL`,
		s.workspaceID, id, expectedRevision, blob, nullTime(expiresAt), keyVersion)
	if err != nil {
		return model.Connection{}, err
	}
	if command.RowsAffected() == 0 {
		return model.Connection{}, ErrConflict
	}
	return s.Connection(ctx, id)
}

func (s *Store) MarkConnectionVerified(ctx context.Context, id string) (model.Connection, error) {
	command, err := s.pool.Exec(ctx, `
		UPDATE connections SET status='active', last_verified_at=now(),
		       last_error_code=NULL, last_error_message=NULL, updated_at=now()
		WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, s.workspaceID, id)
	if err != nil {
		return model.Connection{}, err
	}
	if command.RowsAffected() == 0 {
		return model.Connection{}, ErrNotFound
	}
	return s.Connection(ctx, id)
}

func (s *Store) MarkConnectionUsed(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, `UPDATE connections SET last_used_at = now() WHERE workspace_id = $1 AND id = $2 AND (last_used_at IS NULL OR last_used_at < now()-interval '1 minute')`, s.workspaceID, id)
}

func (s *Store) MarkConnectionError(ctx context.Context, id, code, message string) {
	_, _ = s.pool.Exec(ctx, `
		UPDATE connections SET status = 'error', last_error_code = $3, last_error_message = $4, updated_at = now()
		WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id, code, message)
}

func (s *Store) MarkConnectionConfigured(ctx context.Context, id string) (model.Connection, error) {
	_, err := s.pool.Exec(ctx, `UPDATE connections SET status='active', last_verified_at=NULL,last_error_code=NULL,last_error_message=NULL WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, s.workspaceID, id)
	if err != nil {
		return model.Connection{}, err
	}
	return s.Connection(ctx, id)
}
