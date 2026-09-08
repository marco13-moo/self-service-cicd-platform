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
	{version: 3, sql: `
CREATE TABLE IF NOT EXISTS tenants (
  id TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO tenants(id) VALUES('default') ON CONFLICT DO NOTHING;

ALTER TABLE services ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE environments ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE scm_deliveries ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE scm_commands ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE services ADD CONSTRAINT services_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants(id);
ALTER TABLE environments ADD CONSTRAINT environments_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants(id);
ALTER TABLE scm_deliveries ADD CONSTRAINT scm_deliveries_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants(id);
ALTER TABLE scm_commands ADD CONSTRAINT scm_commands_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE services DROP CONSTRAINT IF EXISTS services_pkey;
ALTER TABLE services ADD PRIMARY KEY (tenant_id,name);
ALTER TABLE environments DROP CONSTRAINT IF EXISTS environments_pkey;
ALTER TABLE environments ADD PRIMARY KEY (tenant_id,name);
ALTER TABLE scm_deliveries DROP CONSTRAINT IF EXISTS scm_deliveries_pkey;
ALTER TABLE scm_deliveries ADD PRIMARY KEY (tenant_id,provider,delivery_id);
ALTER TABLE scm_commands DROP CONSTRAINT IF EXISTS scm_commands_pkey;
ALTER TABLE scm_commands ADD PRIMARY KEY (tenant_id,id);

CREATE INDEX IF NOT EXISTS services_tenant_repository_idx ON services (tenant_id,(lower(document->>'repo_url')));
CREATE UNIQUE INDEX IF NOT EXISTS services_repository_owner_idx ON services (
  (lower((document->'repository'->>'provider') || ':' || (document->'repository'->>'workspace') || '/' || (document->'repository'->>'name')))
) WHERE coalesce(document->'repository'->>'provider','') <> '';
CREATE INDEX IF NOT EXISTS environments_tenant_service_idx ON environments (tenant_id,(document->'spec'->>'service'));
CREATE INDEX IF NOT EXISTS scm_commands_tenant_lease_idx ON scm_commands(tenant_id,status,available_at,created_at);

ALTER TABLE services ENABLE ROW LEVEL SECURITY;
ALTER TABLE environments ENABLE ROW LEVEL SECURITY;
ALTER TABLE scm_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE scm_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE services FORCE ROW LEVEL SECURITY;
ALTER TABLE environments FORCE ROW LEVEL SECURITY;
ALTER TABLE scm_deliveries FORCE ROW LEVEL SECURITY;
ALTER TABLE scm_commands FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS services_tenant_isolation ON services;
CREATE POLICY services_tenant_isolation ON services USING (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true)) WITH CHECK (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true));
DROP POLICY IF EXISTS environments_tenant_isolation ON environments;
CREATE POLICY environments_tenant_isolation ON environments USING (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true)) WITH CHECK (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true));
DROP POLICY IF EXISTS scm_deliveries_tenant_isolation ON scm_deliveries;
CREATE POLICY scm_deliveries_tenant_isolation ON scm_deliveries USING (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true)) WITH CHECK (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true));
DROP POLICY IF EXISTS scm_commands_tenant_isolation ON scm_commands;
CREATE POLICY scm_commands_tenant_isolation ON scm_commands USING (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true)) WITH CHECK (current_setting('app.bypass_rls',true)='on' OR tenant_id=current_setting('app.tenant_id',true));
`},
	{version: 4, sql: `
-- Tenant-auth configuration persisted per-tenant.
CREATE TABLE IF NOT EXISTS tenant_auths (
  tenant_id TEXT PRIMARY KEY REFERENCES tenants(id),
  config JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
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

// beginTenantTx binds an authorization scope to one physical PostgreSQL
// connection for exactly one transaction, preventing pool reuse from carrying
// identity between requests. Privileged mode is reserved for the reconciler's
// global lease scheduler and repository-to-tenant webhook resolution.
func beginTenantTx(ctx context.Context, db *sql.DB, tenantID TenantID, privileged bool) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	bypass := "off"
	if privileged {
		bypass = "on"
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.bypass_rls',$2,true)`, string(normalizeTenantID(tenantID)), bypass); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("bind tenant transaction: %w", err)
	}
	return tx, nil
}
