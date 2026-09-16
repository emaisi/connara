package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

//go:embed schema.sql
var schema string

//go:embed migrations/002_hardening.sql
var hardeningSchema string

type Store struct {
	pool        *pgxpool.Pool
	workspaceID string
}

func Open(ctx context.Context, databaseURL, workspaceID string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errors.New("workspace ID is required")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL URL: %w", err)
	}
	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	store := &Store{pool: pool, workspaceID: workspaceID}
	if err := store.Ready(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ready(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return nil
}

func (s *Store) WorkspaceID() string { return s.workspaceID }

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(714031415926)`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(714031415926)`) }()
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("ensure schema migrations: %w", err)
	}

	for index, migration := range []string{schema, hardeningSchema} {
		version := index + 1
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		results, err := conn.Conn().PgConn().Exec(ctx, migration).ReadAll()
		if err != nil {
			return fmt.Errorf("apply schema v%d: %w", version, err)
		}
		for _, result := range results {
			if result.Err != nil {
				return fmt.Errorf("apply schema v%d: %w", version, result.Err)
			}
		}
	}

	return nil
}

func StableID(namespace, key string) string {
	sum := sha256.Sum256([]byte(namespace + ":" + key))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func IsUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func jsonOrObject(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`{}`)
	}
	return value
}

func jsonOrArray(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`[]`)
	}
	return value
}

func (s *Store) CheckSchema(ctx context.Context) error {
	var latest int
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&latest); err != nil {
		return fmt.Errorf("run apihub-init with migration credentials first: %w", err)
	}
	if latest != 2 {
		return fmt.Errorf("schema version %d unsupported; run apihub-init (expected 2)", latest)
	}
	return nil
}
