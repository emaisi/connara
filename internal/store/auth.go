package store

import (
	"context"
	"fmt"
	"strings"

	"apihub-go/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListAuthTemplates(ctx context.Context) ([]model.AuthTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, template_key, name, source, flow_type, status,
		       credential_schema, token_request, injection_rules,
		       version, created_at, updated_at
		FROM auth_templates
		WHERE workspace_id = $1 AND deleted_at IS NULL
		ORDER BY CASE source WHEN 'builtin' THEN 0 ELSE 1 END, name, id`, s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuthTemplate, 0)
	for rows.Next() {
		item, err := scanAuthTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AuthTemplate(ctx context.Context, idOrKey string) (model.AuthTemplate, error) {
	item, err := scanAuthTemplate(s.pool.QueryRow(ctx, `
		SELECT id::text, template_key, name, source, flow_type, status,
		       credential_schema, token_request, injection_rules,
		       version, created_at, updated_at
		FROM auth_templates
		WHERE workspace_id = $1 AND deleted_at IS NULL
		  AND (id::text = $2 OR template_key = $2)`, s.workspaceID, idOrKey))
	return item, mapNotFound(err)
}

func scanAuthTemplate(row rowScanner) (model.AuthTemplate, error) {
	var item model.AuthTemplate
	err := row.Scan(
		&item.ID, &item.TemplateKey, &item.Name, &item.Source, &item.FlowType, &item.Status,
		&item.CredentialSchema, &item.TokenRequest, &item.InjectionRules,
		&item.Version, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) SaveAuthTemplate(ctx context.Context, item model.AuthTemplate) (model.AuthTemplate, error) {
	if item.ID == "" {
		item.ID = StableID("auth-template-custom", s.workspaceID+":"+item.TemplateKey)
	}
	if item.Source == "" {
		item.Source = "custom"
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_templates(
			id, workspace_id, template_key, name, source, flow_type, status,
			credential_schema, token_request, injection_rules
		) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT(id) DO UPDATE SET
			template_key = EXCLUDED.template_key, name = EXCLUDED.name,
			flow_type = EXCLUDED.flow_type, status = EXCLUDED.status,
			credential_schema = EXCLUDED.credential_schema,
			token_request = EXCLUDED.token_request,
			injection_rules = EXCLUDED.injection_rules,
			version = auth_templates.version + 1, updated_at = now()
		WHERE auth_templates.workspace_id = EXCLUDED.workspace_id AND auth_templates.source = 'custom'`,
		item.ID, s.workspaceID, item.TemplateKey, item.Name, item.Source, item.FlowType, item.Status,
		jsonOrObject(item.CredentialSchema), jsonOrObject(item.TokenRequest), jsonOrArray(item.InjectionRules))
	if err != nil {
		return model.AuthTemplate{}, err
	}
	return s.AuthTemplate(ctx, item.ID)
}

func (s *Store) SetAuthTemplateStatus(ctx context.Context, id, status string) (model.AuthTemplate, error) {
	command, err := s.pool.Exec(ctx, `
		UPDATE auth_templates SET status = $3, version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id, status)
	if err != nil {
		return model.AuthTemplate{}, err
	}
	if command.RowsAffected() == 0 {
		return model.AuthTemplate{}, ErrNotFound
	}
	return s.AuthTemplate(ctx, id)
}

func (s *Store) DeleteAuthTemplate(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var source string
		if err := tx.QueryRow(ctx, `SELECT source FROM auth_templates WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id).Scan(&source); err != nil {
			return mapNotFound(err)
		}
		if source != "custom" {
			return fmt.Errorf("%w: built-in auth templates cannot be deleted", ErrConflict)
		}
		var used bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM auth_instances WHERE workspace_id = $1 AND auth_template_id = $2 AND deleted_at IS NULL)
			    OR EXISTS(SELECT 1 FROM system_auth_templates WHERE workspace_id = $1 AND auth_template_id = $2)`,
			s.workspaceID, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return fmt.Errorf("%w: auth template is in use", ErrConflict)
		}
		_, err := tx.Exec(ctx, `UPDATE auth_templates SET deleted_at = now(), updated_at = now() WHERE workspace_id = $1 AND id = $2`, s.workspaceID, id)
		return err
	})
}

func (s *Store) ListAuthInstances(ctx context.Context, systemID string) ([]model.AuthInstance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ai.id::text, ai.instance_key, ai.name, ai.system_id::text, s.system_key,
		       ai.auth_template_id::text, at.template_key, at.flow_type, ai.status,
		       COALESCE(ai.token_url, ''), COALESCE(ai.refresh_url, ''),
		       COALESCE(ai.token_path, ''), COALESCE(ai.expiry_path, ''),
		       COALESCE(ai.header_name, ''), COALESCE(ai.header_value_template, ''),
		       ai.public_config, ai.secret_blob, ai.key_version, ai.version,
		       ai.last_tested_at, COALESCE(ai.last_test_status, ''), ai.created_at, ai.updated_at
		FROM auth_instances ai
		JOIN systems s ON s.id = ai.system_id AND s.workspace_id = ai.workspace_id
		JOIN auth_templates at ON at.id = ai.auth_template_id AND at.workspace_id = ai.workspace_id
		WHERE ai.workspace_id = $1 AND ai.deleted_at IS NULL
		  AND ($2 = '' OR ai.system_id::text = $2 OR s.system_key = $2)
		ORDER BY ai.updated_at DESC, ai.id DESC`, s.workspaceID, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuthInstance, 0)
	for rows.Next() {
		item, err := scanAuthInstance(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AuthInstance(ctx context.Context, id string) (model.AuthInstance, error) {
	item, err := scanAuthInstance(s.pool.QueryRow(ctx, `
		SELECT ai.id::text, ai.instance_key, ai.name, ai.system_id::text, s.system_key,
		       ai.auth_template_id::text, at.template_key, at.flow_type, ai.status,
		       COALESCE(ai.token_url, ''), COALESCE(ai.refresh_url, ''),
		       COALESCE(ai.token_path, ''), COALESCE(ai.expiry_path, ''),
		       COALESCE(ai.header_name, ''), COALESCE(ai.header_value_template, ''),
		       ai.public_config, ai.secret_blob, ai.key_version, ai.version,
		       ai.last_tested_at, COALESCE(ai.last_test_status, ''), ai.created_at, ai.updated_at
		FROM auth_instances ai
		JOIN systems s ON s.id = ai.system_id AND s.workspace_id = ai.workspace_id
		JOIN auth_templates at ON at.id = ai.auth_template_id AND at.workspace_id = ai.workspace_id
		WHERE ai.workspace_id = $1 AND ai.id = $2 AND ai.deleted_at IS NULL`, s.workspaceID, id))
	return item, mapNotFound(err)
}

func scanAuthInstance(row rowScanner) (model.AuthInstance, error) {
	var item model.AuthInstance
	err := row.Scan(
		&item.ID, &item.InstanceKey, &item.Name, &item.SystemID, &item.SystemKey,
		&item.AuthTemplateID, &item.AuthTemplateKey, &item.AuthTemplateFlow, &item.Status,
		&item.TokenURL, &item.RefreshURL, &item.TokenPath, &item.ExpiryPath,
		&item.HeaderName, &item.HeaderValueTemplate, &item.PublicConfig, &item.SecretBlob,
		&item.KeyVersion, &item.Version, &item.LastTestedAt, &item.LastTestStatus,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) SaveAuthInstance(ctx context.Context, item model.AuthInstance) (model.AuthInstance, error) {
	if item.ID == "" {
		item.ID = StableID("auth-instance", s.workspaceID+":"+item.InstanceKey)
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var compatible bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM system_auth_templates sat
				JOIN systems s ON s.id = sat.system_id AND s.workspace_id = sat.workspace_id AND s.deleted_at IS NULL
				JOIN auth_templates at ON at.id = sat.auth_template_id AND at.workspace_id = sat.workspace_id AND at.deleted_at IS NULL
				WHERE sat.workspace_id = $1 AND sat.system_id = $2 AND sat.auth_template_id = $3
			)`, s.workspaceID, item.SystemID, item.AuthTemplateID).Scan(&compatible); err != nil {
			return err
		}
		if !compatible {
			return fmt.Errorf("%w: system does not allow this auth template", ErrConflict)
		}
		if item.Status == "ready" {
			var templateStatus string
			if err := tx.QueryRow(ctx, `
				SELECT status FROM auth_templates
				WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`,
				s.workspaceID, item.AuthTemplateID).Scan(&templateStatus); err != nil {
				return mapNotFound(err)
			}
			if templateStatus != "published" {
				return fmt.Errorf("%w: ready auth instances require a published template", ErrConflict)
			}
		}
		command, err := tx.Exec(ctx, `
			INSERT INTO auth_instances(
				id, workspace_id, instance_key, name, system_id, auth_template_id, status,
				token_url, refresh_url, token_path, expiry_path, header_name, header_value_template,
				public_config, secret_blob, key_version
			) VALUES($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''),
			         NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), $14, $15, $16)
			ON CONFLICT(id) DO UPDATE SET
				instance_key = EXCLUDED.instance_key, name = EXCLUDED.name,
				system_id = EXCLUDED.system_id, auth_template_id = EXCLUDED.auth_template_id,
				status = EXCLUDED.status, token_url = EXCLUDED.token_url,
				refresh_url = EXCLUDED.refresh_url, token_path = EXCLUDED.token_path,
				expiry_path = EXCLUDED.expiry_path, header_name = EXCLUDED.header_name,
				header_value_template = EXCLUDED.header_value_template,
				public_config = EXCLUDED.public_config,
				secret_blob = CASE WHEN EXCLUDED.secret_blob IS NULL THEN auth_instances.secret_blob ELSE EXCLUDED.secret_blob END,
				key_version = CASE WHEN EXCLUDED.secret_blob IS NULL THEN auth_instances.key_version ELSE EXCLUDED.key_version END, version = auth_instances.version + 1, updated_at = now()
			WHERE auth_instances.workspace_id = EXCLUDED.workspace_id AND auth_instances.version=$17
 AND ( (auth_instances.system_id=EXCLUDED.system_id AND auth_instances.auth_template_id=EXCLUDED.auth_template_id)
 OR NOT EXISTS(SELECT 1 FROM integrations WHERE workspace_id=$2 AND auth_instance_id=$1 AND deleted_at IS NULL))`,
			item.ID, s.workspaceID, item.InstanceKey, item.Name, item.SystemID, item.AuthTemplateID,
			item.Status, item.TokenURL, item.RefreshURL, item.TokenPath, item.ExpiryPath,
			item.HeaderName, item.HeaderValueTemplate, jsonOrObject(item.PublicConfig), item.SecretBlob, item.KeyVersion, item.Version)
		if err == nil && command.RowsAffected() == 0 {
			return ErrConflict
		}
		return err
	})
	if err != nil {
		return model.AuthInstance{}, err
	}
	return s.AuthInstance(ctx, item.ID)
}

func (s *Store) SetAuthInstanceTestResult(ctx context.Context, id, status string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE auth_instances SET last_tested_at = now(), last_test_status = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL`, s.workspaceID, id, strings.TrimSpace(status))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
