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

func (s *Store) ListSystemGroups(ctx context.Context) ([]model.SystemGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, name, sort_order, created_at, updated_at
		FROM system_groups
		WHERE workspace_id = $1 AND deleted_at IS NULL
		ORDER BY sort_order, name, id`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SystemGroup, 0)
	for rows.Next() {
		var item model.SystemGroup
		if err := rows.Scan(&item.ID, &item.Name, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveSystemGroup(ctx context.Context, item model.SystemGroup) (model.SystemGroup, error) {
	if item.ID == "" {
		item.ID = StableID("system-group-custom", s.workspaceID+":"+strings.ToLower(item.Name))
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO system_groups(id, workspace_id, name, sort_order)
		VALUES($1, $2, $3, $4)
		ON CONFLICT(id) DO UPDATE SET
			name = EXCLUDED.name, sort_order = EXCLUDED.sort_order, updated_at = now()
		WHERE system_groups.workspace_id = EXCLUDED.workspace_id
		RETURNING id::text, name, sort_order, created_at, updated_at`,
		item.ID, s.workspaceID, strings.TrimSpace(item.Name), item.SortOrder,
	).Scan(&item.ID, &item.Name, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) DeleteSystemGroup(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var used bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM systems
				WHERE workspace_id = $1 AND group_id = $2 AND deleted_at IS NULL
			)`, s.workspaceID, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return fmt.Errorf("%w: system group is in use", ErrConflict)
		}
		command, err := tx.Exec(ctx, `
			UPDATE system_groups SET deleted_at = now(), updated_at = now()
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) ListSystems(ctx context.Context, options ...ListOptions) ([]model.System, error) {
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, `
		SELECT
			s.id::text, s.system_key, s.name, s.source,
			COALESCE(s.group_id::text, ''), COALESCE(g.name, ''), s.description,
			COALESCE(s.homepage_url, ''), COALESCE(s.icon_key, ''), s.status,
			COALESCE(s.catalog_version, ''), s.version, s.created_at, s.updated_at,
			COALESCE((
				SELECT array_agg(sat.auth_template_id::text ORDER BY sat.auth_template_id::text)
				FROM system_auth_templates sat
				WHERE sat.workspace_id = s.workspace_id AND sat.system_id = s.id
			), '{}'::text[]),
			(SELECT count(*) FROM actions a WHERE a.workspace_id = s.workspace_id AND a.system_id = s.id AND a.deleted_at IS NULL),
			(SELECT count(*) FROM actions a WHERE a.workspace_id = s.workspace_id AND a.system_id = s.id AND a.executable AND a.status = 'active' AND a.deleted_at IS NULL),
			(SELECT count(*) FROM connections c JOIN integrations i ON i.id = c.integration_id
			 WHERE c.workspace_id = s.workspace_id AND i.system_id = s.id AND c.deleted_at IS NULL AND i.deleted_at IS NULL)
		FROM systems s
		LEFT JOIN system_groups g ON g.id = s.group_id AND g.workspace_id = s.workspace_id AND g.deleted_at IS NULL
		WHERE s.workspace_id = $1 AND s.deleted_at IS NULL
		AND ($2='' OR (s.updated_at,s.id)<($3,NULLIF($2,'')::uuid))
 AND ($4='' OR s.system_key ILIKE '%'||$4||'%' OR s.name ILIKE '%'||$4||'%' OR s.description ILIKE '%'||$4||'%' OR g.name ILIKE '%'||$4||'%'
 OR EXISTS(SELECT 1 FROM auth_instances ai JOIN auth_templates at ON at.id=ai.auth_template_id AND at.workspace_id=ai.workspace_id WHERE ai.workspace_id=s.workspace_id AND ai.system_id=s.id AND ai.deleted_at IS NULL AND (ai.name ILIKE '%'||$4||'%' OR at.name ILIKE '%'||$4||'%'))
 OR EXISTS(SELECT 1 FROM system_auth_templates sat JOIN auth_templates at ON at.id=sat.auth_template_id AND at.workspace_id=sat.workspace_id WHERE sat.workspace_id=s.workspace_id AND sat.system_id=s.id AND at.name ILIKE '%'||$4||'%'))
 AND ($7='' OR g.name=$7)
 AND ($8='' OR ($8='connected' AND EXISTS(SELECT 1 FROM connections c JOIN integrations i ON i.id=c.integration_id AND i.workspace_id=c.workspace_id WHERE c.workspace_id=s.workspace_id AND i.system_id=s.id AND c.deleted_at IS NULL AND i.deleted_at IS NULL)) OR ($8='custom' AND s.source='custom'))
 AND ($5='' OR s.status=$5)
 ORDER BY s.updated_at DESC,s.id DESC LIMIT $6`, s.workspaceID, o.ID, o.Before, o.Query, o.Status, o.Limit, o.Group, o.Scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.System, 0)
	for rows.Next() {
		item, err := scanSystem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) System(ctx context.Context, idOrKey string) (model.System, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			s.id::text, s.system_key, s.name, s.source,
			COALESCE(s.group_id::text, ''), COALESCE(g.name, ''), s.description,
			COALESCE(s.homepage_url, ''), COALESCE(s.icon_key, ''), s.status,
			COALESCE(s.catalog_version, ''), s.version, s.created_at, s.updated_at,
			COALESCE((SELECT array_agg(sat.auth_template_id::text ORDER BY sat.auth_template_id::text)
			 FROM system_auth_templates sat WHERE sat.workspace_id = s.workspace_id AND sat.system_id = s.id), '{}'::text[]),
			(SELECT count(*) FROM actions a WHERE a.workspace_id = s.workspace_id AND a.system_id = s.id AND a.deleted_at IS NULL),
			(SELECT count(*) FROM actions a WHERE a.workspace_id = s.workspace_id AND a.system_id = s.id AND a.executable AND a.status = 'active' AND a.deleted_at IS NULL),
			(SELECT count(*) FROM connections c JOIN integrations i ON i.id = c.integration_id
			 WHERE c.workspace_id = s.workspace_id AND i.system_id = s.id AND c.deleted_at IS NULL AND i.deleted_at IS NULL)
		FROM systems s
		LEFT JOIN system_groups g ON g.id = s.group_id AND g.workspace_id = s.workspace_id AND g.deleted_at IS NULL
		WHERE s.workspace_id = $1 AND s.deleted_at IS NULL
		  AND (s.id::text = $2 OR s.system_key = $2)`, s.workspaceID, idOrKey)
	item, err := scanSystem(row)
	return item, mapNotFound(err)
}

type rowScanner interface{ Scan(...any) error }

func scanSystem(row rowScanner) (model.System, error) {
	var item model.System
	err := row.Scan(
		&item.ID, &item.SystemKey, &item.Name, &item.Source, &item.GroupID, &item.GroupName,
		&item.Description, &item.HomepageURL, &item.IconKey, &item.Status, &item.CatalogVersion,
		&item.Version, &item.CreatedAt, &item.UpdatedAt, &item.AuthTemplateIDs,
		&item.ActionCount, &item.ExecutableCount, &item.ConnectionCount,
	)
	return item, err
}

func (s *Store) CreateSystem(ctx context.Context, item model.System, defaultAuthTemplateID string) (model.System, error) {
	item.ID = StableID("system-custom", s.workspaceID+":"+item.SystemKey)
	item.Source = "custom"
	item.Status = "active"
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var groupExists, templateExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM system_groups WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL)`, s.workspaceID, item.GroupID).Scan(&groupExists); err != nil {
			return err
		}
		if !groupExists {
			return fmt.Errorf("%w: system group not found", ErrNotFound)
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_templates WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL AND status = 'published')`, s.workspaceID, defaultAuthTemplateID).Scan(&templateExists); err != nil {
			return err
		}
		if !templateExists {
			return fmt.Errorf("%w: auth template not found", ErrNotFound)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO systems(id, workspace_id, system_key, name, source, group_id, description, homepage_url, icon_key, status)
			VALUES($1, $2, $3, $4, 'custom', $5, $6, NULLIF($7, ''), NULLIF($8, ''), 'active')`,
			item.ID, s.workspaceID, item.SystemKey, item.Name, item.GroupID, item.Description, item.HomepageURL, item.IconKey); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO system_auth_templates(id, workspace_id, system_id, auth_template_id, is_default)
			VALUES($1, $2, $3, $4, true)`, StableID("system-auth", item.ID+":"+defaultAuthTemplateID),
			s.workspaceID, item.ID, defaultAuthTemplateID)
		return err
	})
	if err != nil {
		return model.System{}, err
	}
	return s.System(ctx, item.ID)
}

func (s *Store) UpdateSystemGroup(ctx context.Context, systemID, groupID string) (model.System, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var groupExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM system_groups
				WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
			)`, s.workspaceID, groupID).Scan(&groupExists); err != nil {
			return err
		}
		if !groupExists {
			return fmt.Errorf("%w: system group not found", ErrNotFound)
		}
		command, err := tx.Exec(ctx, `
			UPDATE systems
			SET group_id = $3, version = version + 1, updated_at = now()
			WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, systemID, groupID)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return model.System{}, err
	}
	return s.System(ctx, systemID)
}

func (s *Store) SetSystemAuthTemplates(ctx context.Context, systemID string, templateIDs []string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var systemExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM systems WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL)`, s.workspaceID, systemID).Scan(&systemExists); err != nil {
			return err
		}
		if !systemExists {
			return ErrNotFound
		}
		if len(templateIDs) == 0 {
			return fmt.Errorf("%w: at least one auth template is required", ErrConflict)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM auth_templates WHERE workspace_id = $1 AND id = ANY($2::uuid[]) AND deleted_at IS NULL AND status = 'published'`, s.workspaceID, templateIDs).Scan(&count); err != nil {
			return err
		}
		if count != len(templateIDs) {
			return fmt.Errorf("%w: one or more auth templates are invalid", ErrNotFound)
		}
		var incompatibleInstances int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM auth_instances
			WHERE workspace_id = $1 AND system_id = $2 AND deleted_at IS NULL
			  AND NOT (auth_template_id = ANY($3::uuid[]))`,
			s.workspaceID, systemID, templateIDs).Scan(&incompatibleInstances); err != nil {
			return err
		}
		if incompatibleInstances > 0 {
			return fmt.Errorf("%w: auth templates used by existing instances cannot be removed", ErrConflict)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM system_auth_templates WHERE workspace_id = $1 AND system_id = $2`, s.workspaceID, systemID); err != nil {
			return err
		}
		for index, templateID := range templateIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO system_auth_templates(id, workspace_id, system_id, auth_template_id, is_default)
				VALUES($1, $2, $3, $4, $5)`, StableID("system-auth", systemID+":"+templateID),
				s.workspaceID, systemID, templateID, index == 0); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListActions(ctx context.Context, query, systemID string, options ...ListOptions) ([]model.ActionDefinition, error) {
	query = strings.TrimSpace(query)
	o := listOptions(options)
	rows, err := s.pool.Query(ctx, `
		SELECT a.id::text, a.action_key, a.name, a.description, a.source,
		       a.system_id::text, s.system_key, COALESCE(a.integration_id::text, ''),
		       a.http_method, a.relative_path, a.required_scopes,
		       a.input_schema, a.output_schema, a.example_input,
		       a.executable, a.status, a.version, a.created_at, a.updated_at
		FROM actions a
		JOIN systems s ON s.id = a.system_id AND s.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1 AND a.deleted_at IS NULL
		  AND ($2 = '' OR a.system_id::text = $2 OR s.system_key = $2)
		  AND ($3 = '' OR a.action_key ILIKE '%' || $3 || '%' OR a.name ILIKE '%' || $3 || '%' OR a.description ILIKE '%' || $3 || '%' OR a.relative_path ILIKE '%'||$3||'%' OR a.http_method ILIKE '%'||$3||'%' OR s.name ILIKE '%'||$3||'%')
		AND ($4='' OR (a.updated_at,a.id)<($5,NULLIF($4,'')::uuid)) AND ($6='' OR a.status=$6) AND ($8='' OR a.executable=($8='true'))
 ORDER BY a.updated_at DESC,a.id DESC LIMIT $7`, s.workspaceID, systemID, query, o.ID, o.Before, o.Status, o.Limit, o.Executable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ActionDefinition, 0)
	for rows.Next() {
		var item model.ActionDefinition
		if err := rows.Scan(
			&item.ID, &item.ActionKey, &item.Name, &item.Description, &item.Source,
			&item.SystemID, &item.SystemKey, &item.IntegrationID, &item.HTTPMethod,
			&item.RelativePath, &item.RequiredScopes, &item.InputSchema, &item.OutputSchema,
			&item.ExampleInput, &item.Executable, &item.Status, &item.Version,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Action(ctx context.Context, idOrKey string) (model.ActionDefinition, error) {
	var item model.ActionDefinition
	err := s.pool.QueryRow(ctx, `
		SELECT a.id::text, a.action_key, a.name, a.description, a.source,
		       a.system_id::text, s.system_key, COALESCE(a.integration_id::text, ''),
		       a.http_method, a.relative_path, a.required_scopes,
		       a.input_schema, a.output_schema, a.example_input,
		       a.executable, a.status, a.version, a.created_at, a.updated_at
		FROM actions a
		JOIN systems s ON s.id = a.system_id AND s.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1 AND a.deleted_at IS NULL AND (a.id::text = $2 OR a.action_key = $2)`,
		s.workspaceID, idOrKey,
	).Scan(
		&item.ID, &item.ActionKey, &item.Name, &item.Description, &item.Source,
		&item.SystemID, &item.SystemKey, &item.IntegrationID, &item.HTTPMethod,
		&item.RelativePath, &item.RequiredScopes, &item.InputSchema, &item.OutputSchema,
		&item.ExampleInput, &item.Executable, &item.Status, &item.Version,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, mapNotFound(err)
}

func (s *Store) SaveAction(ctx context.Context, item model.ActionDefinition) (model.ActionDefinition, error) {
	if item.ID == "" {
		item.ID = StableID("action-custom", s.workspaceID+":"+item.ActionKey)
	}
	if item.Name == "" {
		item.Name = item.ActionKey
	}
	if item.Status == "" {
		item.Status = "active"
	}
	item.Source = "custom"
	item.Executable = true
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var systemID string
		if item.IntegrationID != "" {
			if err := tx.QueryRow(ctx, `SELECT system_id::text FROM integrations WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, item.IntegrationID).Scan(&systemID); err != nil {
				return mapNotFound(err)
			}
			if item.SystemID != "" && item.SystemID != systemID {
				return fmt.Errorf("%w: integration and system mismatch", ErrConflict)
			}
			item.SystemID = systemID
		}
		if item.SystemID == "" {
			return fmt.Errorf("%w: system is required", ErrConflict)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO actions(
				id, workspace_id, action_key, name, description, source, system_id, integration_id,
				http_method, relative_path, required_scopes, input_schema, output_schema,
				example_input, executable, status
			) VALUES($1, $2, $3, $4, $5, 'custom', $6, NULLIF($7, '')::uuid,
			         $8, $9, $10, $11, $12, $13, true, $14)
			ON CONFLICT(id) DO UPDATE SET
				action_key = EXCLUDED.action_key, name = EXCLUDED.name,
				description = EXCLUDED.description, system_id = EXCLUDED.system_id,
				integration_id = EXCLUDED.integration_id, http_method = EXCLUDED.http_method,
				relative_path = EXCLUDED.relative_path, required_scopes = EXCLUDED.required_scopes,
				input_schema = EXCLUDED.input_schema, output_schema = EXCLUDED.output_schema,
				example_input = EXCLUDED.example_input, status = EXCLUDED.status,
				version = actions.version + 1, updated_at = now()
			WHERE actions.workspace_id = EXCLUDED.workspace_id AND actions.source = 'custom'`,
			item.ID, s.workspaceID, item.ActionKey, item.Name, item.Description, item.SystemID,
			item.IntegrationID, strings.ToUpper(item.HTTPMethod), item.RelativePath, item.RequiredScopes,
			jsonOrObject(item.InputSchema), jsonOrObject(item.OutputSchema), jsonOrObject(item.ExampleInput), item.Status)
		return err
	})
	if err != nil {
		return model.ActionDefinition{}, err
	}
	return s.Action(ctx, item.ID)
}

func decodeObject(value json.RawMessage) json.RawMessage { return jsonOrObject(value) }

func utcNow() time.Time { return time.Now().UTC() }
