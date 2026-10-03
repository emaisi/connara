package store

import (
	"context"
	"encoding/json"
)

// CatalogLookups contains only identifiers, labels and state needed by selectors.
// List pages continue to use paginated resource endpoints for full details.
func (s *Store) CatalogLookups(ctx context.Context) (map[string]json.RawMessage, error) {
	var systems, connections []byte
	err := s.database(ctx).QueryRow(ctx, `
		WITH action_counts AS (
			SELECT system_id, count(*) AS total,
			       count(*) FILTER (WHERE executable AND status='active') AS executable
			FROM actions WHERE workspace_id=$1 AND deleted_at IS NULL GROUP BY system_id
		), connection_counts AS (
			SELECT i.system_id, count(*) AS total
			FROM connections c
			JOIN integrations i ON i.id=c.integration_id AND i.workspace_id=c.workspace_id
			WHERE c.workspace_id=$1 AND c.deleted_at IS NULL AND i.deleted_at IS NULL
			GROUP BY i.system_id
		), template_ids AS (
			SELECT system_id, jsonb_agg(auth_template_id ORDER BY auth_template_id) AS ids,
			       max(auth_template_id::text) FILTER (WHERE is_default) AS default_id
			FROM system_auth_templates WHERE workspace_id=$1 GROUP BY system_id
		), system_rows AS (
			SELECT s.id,s.system_key AS "systemKey",s.name,s.source,s.group_id AS "groupId",
			       g.name AS "groupName",s.description,s.status,
			       COALESCE(t.ids,'[]'::jsonb) AS "authTemplateIds",
			       COALESCE(t.default_id,'') AS "defaultAuthTemplateId",
			       COALESCE(a.total,0) AS "actionCount",
			       COALESCE(a.executable,0) AS "executableCount",
			       COALESCE(c.total,0) AS "connectionCount"
			FROM systems s
			LEFT JOIN system_groups g ON g.id=s.group_id AND g.workspace_id=s.workspace_id AND g.deleted_at IS NULL
			LEFT JOIN action_counts a ON a.system_id=s.id
			LEFT JOIN connection_counts c ON c.system_id=s.id
			LEFT JOIN template_ids t ON t.system_id=s.id
			WHERE s.workspace_id=$1 AND s.deleted_at IS NULL ORDER BY s.name,s.id
		), connection_rows AS (
			SELECT c.id,c.name,c.connection_key AS "connectionKey",c.auth_instance_id AS "authInstanceId",
			       CASE WHEN NOT c.enabled THEN 'disabled' WHEN c.status='active' AND (c.last_verified_at IS NULL OR c.verified_target_version<>i.target_version OR c.verified_revision<>c.revision) THEN 'pending' ELSE c.status END AS status,c.enabled,c.revision,c.verified_revision AS "verifiedRevision",c.verified_target_version AS "verifiedTargetVersion",c.last_verified_at AS "lastVerifiedAt",c.last_used_at AS "lastUsedAt",c.tags,
			       i.integration_key AS "integrationKey",s.system_key AS "systemKey",eu.external_key AS "endUserKey"
			FROM connections c
			JOIN integrations i ON i.id=c.integration_id AND i.workspace_id=c.workspace_id
			JOIN systems s ON s.id=i.system_id AND s.workspace_id=c.workspace_id
			JOIN end_users eu ON eu.id=c.end_user_id AND eu.workspace_id=c.workspace_id
			WHERE c.workspace_id=$1 AND c.deleted_at IS NULL AND i.deleted_at IS NULL
			ORDER BY c.updated_at DESC,c.id DESC
		)
		SELECT COALESCE((SELECT jsonb_agg(row) FROM system_rows row),'[]'::jsonb),
		       COALESCE((SELECT jsonb_agg(row) FROM connection_rows row),'[]'::jsonb)`, s.workspaceID).Scan(&systems, &connections)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{
		"systems":     json.RawMessage(systems),
		"connections": json.RawMessage(connections),
	}, nil
}
