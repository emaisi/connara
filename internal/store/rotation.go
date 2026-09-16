package store

import (
	"apihub-go/internal/secret"
	"context"
	"fmt"
)

// RotateSecrets preserves credential revisions; compare-and-swap prevents a
// background refresh or admin update being overwritten during key rotation.
func (s *Store) RotateSecrets(ctx context.Context, codec *secret.Codec) (int, error) {
	count := 0
	for _, spec := range []struct{ table, column, kind string }{{"connections", "credential_blob", "connection"}, {"auth_instances", "secret_blob", "auth-instance"}, {"webhook_endpoints", "secret_blob", "webhook-endpoint"}, {"webhook_sources", "secret_blob", "webhook-source"}} {
		for {
			revision := "0"
			if spec.table == "connections" {
				revision = "revision"
			}
			query := fmt.Sprintf(`SELECT id::text,%s,%s FROM %s WHERE workspace_id=$1 AND key_version<>$2 AND %s IS NOT NULL AND length(%s)>0 ORDER BY id LIMIT 100`, spec.column, revision, spec.table, spec.column, spec.column)
			rows, err := s.pool.Query(ctx, query, s.workspaceID, codec.Version())
			if err != nil {
				return count, err
			}
			type item struct {
				id       string
				blob     []byte
				revision int64
			}
			items := []item{}
			for rows.Next() {
				var i item
				if err := rows.Scan(&i.id, &i.blob, &i.revision); err != nil {
					rows.Close()
					return count, err
				}
				items = append(items, i)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return count, err
			}
			if len(items) == 0 {
				break
			}
			for _, i := range items {
				aad := []byte(s.workspaceID + ":" + spec.kind + ":" + i.id)
				if spec.kind == "connection" {
					aad = []byte(fmt.Sprintf("%s:connection:%s:%d", s.workspaceID, i.id, i.revision))
				}
				plain, err := codec.Decrypt(i.blob, aad)
				if err != nil {
					return count, fmt.Errorf("rotate %s %s: %w", spec.table, i.id, err)
				}
				blob, err := codec.Encrypt(plain, aad)
				if err != nil {
					return count, err
				}
				tag, err := s.pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s=$3,key_version=$4 WHERE workspace_id=$1 AND id=$2 AND %s=$5`, spec.table, spec.column, spec.column), s.workspaceID, i.id, blob, codec.Version(), i.blob)
				if err != nil {
					return count, err
				}
				count += int(tag.RowsAffected())
			}
		}
	}
	return count, nil
}
