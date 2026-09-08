package api

import (
	"context"
	"database/sql"
	"fmt"
)

const migrationLockID int64 = 73310417001

type databaseMigration struct {
	version int
	sql     string
}

var databaseMigrations = []databaseMigration{
	{version: 1, sql: postgresSchema},
	{version: 2, sql: `
CREATE TABLE IF NOT EXISTS services (
  name TEXT PRIMARY KEY,
  document JSONB NOT NULL,
  version BIGINT NOT NULL CHECK (version > 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS environments (
  name TEXT PRIMARY KEY,
  document JSONB NOT NULL,
  version BIGINT NOT NULL CHECK (version > 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS services_repository_idx ON services ((lower(document->>'repo_url')));
CREATE INDEX IF NOT EXISTS environments_service_idx ON environments ((document->'spec'->>'service'));
`},
}

// migrateDatabase serializes schema evolution across concurrently starting
// replicas. The transaction-scoped advisory lock is released on commit or
// rollback, including process termination during a migration.
func migrateDatabase(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin database migration: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire database migration lock: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, migration := range databaseMigrations {
		var applied bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("inspect migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}
		if _, err = tx.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, migration.version); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit database migrations: %w", err)
	}
	return nil
}
