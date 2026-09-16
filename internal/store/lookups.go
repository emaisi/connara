package store

import (
	"context"
	"encoding/json"
)

// CatalogLookups contains only identifiers, labels and state needed by selectors.
// List pages continue to use paginated resource endpoints for full details.
func (s *Store) CatalogLookups(ctx context.Context) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	queries := map[string]string{
		"systems": `SELECT COALESCE(jsonb_agg(row),'[]'::jsonb) FROM (
 SELECT s.id,s.system_key AS "systemKey",s.name,s.source,s.group_id AS "groupId",g.name AS "groupName",s.description,s.status,
 COALESCE((SELECT jsonb_agg(auth_template_id) FROM system_auth_templates WHERE workspace_id=s.workspace_id AND system_id=s.id),'[]'::jsonb) AS "authTemplateIds",
 (SELECT count(*) FROM actions WHERE workspace_id=s.workspace_id AND system_id=s.id AND deleted_at IS NULL) AS "actionCount",
 (SELECT count(*) FROM actions WHERE workspace_id=s.workspace_id AND system_id=s.id AND deleted_at IS NULL AND executable AND status='active') AS "executableCount",
 (SELECT count(*) FROM connections c JOIN integrations i ON i.id=c.integration_id AND i.workspace_id=c.workspace_id WHERE c.workspace_id=s.workspace_id AND i.system_id=s.id AND c.deleted_at IS NULL AND i.deleted_at IS NULL) AS "connectionCount"
 FROM systems s LEFT JOIN system_groups g ON g.id=s.group_id AND g.workspace_id=s.workspace_id AND g.deleted_at IS NULL WHERE s.workspace_id=$1 AND s.deleted_at IS NULL ORDER BY s.name,s.id) row`,
		"connections": `SELECT COALESCE(jsonb_agg(row),'[]'::jsonb) FROM (
 SELECT c.id,c.connection_key AS "connectionKey",c.auth_instance_id AS "authInstanceId",c.status,c.last_verified_at AS "lastVerifiedAt",c.last_used_at AS "lastUsedAt",c.tags,i.integration_key AS "integrationKey",s.system_key AS "systemKey",eu.external_key AS "endUserKey"
 FROM connections c JOIN integrations i ON i.id=c.integration_id AND i.workspace_id=c.workspace_id JOIN systems s ON s.id=i.system_id AND s.workspace_id=c.workspace_id JOIN end_users eu ON eu.id=c.end_user_id AND eu.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.deleted_at IS NULL AND i.deleted_at IS NULL ORDER BY c.updated_at DESC,c.id DESC) row`,
	}
	for key, query := range queries {
		var value []byte
		if err := s.pool.QueryRow(ctx, query, s.workspaceID).Scan(&value); err != nil {
			return nil, err
		}
		result[key] = json.RawMessage(value)
	}
	return result, nil
}
