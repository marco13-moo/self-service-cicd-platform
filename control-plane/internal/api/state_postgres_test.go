package api

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

func TestPostgresAuthoritativeStateAndReplicaHandoff(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	primaryDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer primaryDB.Close()
	secondaryDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer secondaryDB.Close()
	primary, err := NewPostgresServiceStore(context.Background(), primaryDB)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := NewPostgresServiceStore(context.Background(), secondaryDB)
	if err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceName := "ha-" + suffix
	environmentName := serviceName + "-pr-1"
	t.Cleanup(func() {
		_, _ = secondaryDB.Exec(`DELETE FROM environments WHERE name=$1`, environmentName)
		_, _ = secondaryDB.Exec(`DELETE FROM services WHERE name=$1`, serviceName)
		_, _ = secondaryDB.Exec(`DELETE FROM scm_commands WHERE environment=$1`, environmentName)
		_, _ = secondaryDB.Exec(`DELETE FROM scm_deliveries WHERE delivery_id=$1`, suffix)
	})

	service := Service{Name: serviceName, RepoURL: "https://github.com/acme/" + serviceName, Version: 1}
	if err = primary.Put(service); err != nil {
		t.Fatal(err)
	}
	if _, err = secondary.Get(serviceName); err != nil {
		t.Fatalf("second replica could not read service: %v", err)
	}
	env := &orchestrator.Environment{Spec: orchestrator.EnvironmentSpec{Name: environmentName, Service: serviceName}}
	if err = primary.PutEnvironment(env); err != nil {
		t.Fatal(err)
	}
	first, _ := primary.GetEnvironment(environmentName)
	stale, _ := secondary.GetEnvironment(environmentName)
	first.Spec.Parameters = map[string]string{"writer": "primary"}
	if err = primary.PutEnvironment(first); err != nil {
		t.Fatal(err)
	}
	stale.Spec.Parameters = map[string]string{"writer": "stale-secondary"}
	if err = secondary.PutEnvironment(stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale write was not rejected: %v", err)
	}

	primaryCommands, err := NewPostgresCommandStore(context.Background(), primaryDB)
	if err != nil {
		t.Fatal(err)
	}
	secondaryCommands, err := NewPostgresCommandStore(context.Background(), secondaryDB)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	command := &scm.LifecycleCommand{ID: "github:" + suffix, Provider: scm.ProviderGitHub, DeliveryID: suffix, Type: scm.EnsurePreviewEnvironment, Repository: "acme/" + serviceName, PullRequest: 1, Environment: environmentName, Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if duplicate, recordErr := primaryCommands.RecordSCMDelivery(scm.ProviderGitHub, suffix, command, now); recordErr != nil || duplicate {
		t.Fatalf("record command: duplicate=%v err=%v", duplicate, recordErr)
	}
	leased, err := primaryCommands.LeaseSCMCommand(now, time.Second)
	if err != nil || leased.ID != command.ID {
		t.Fatalf("primary lease: %#v %v", leased, err)
	}
	// Simulate abrupt replica termination: its pool disappears without completing
	// the lease. A second replica must reclaim it after the durable expiry.
	if err = primaryDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err = primary.Ready(context.Background()); err == nil {
		t.Fatal("readiness remained healthy after the PostgreSQL pool closed")
	}
	reclaimed, err := secondaryCommands.LeaseSCMCommand(now.Add(2*time.Second), time.Minute)
	if err != nil || reclaimed.ID != command.ID || reclaimed.Attempts != 2 {
		t.Fatalf("secondary lease handoff: %#v %v", reclaimed, err)
	}
	if err = secondaryCommands.CompleteSCMCommand(command.ID, nil, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if duplicate, recordErr := secondaryCommands.RecordSCMDelivery(scm.ProviderGitHub, suffix, command, now); recordErr != nil || !duplicate {
		t.Fatalf("delivery was not deduplicated after handoff: duplicate=%v err=%v", duplicate, recordErr)
	}
}

func TestConcurrentMigrationStartup(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	errorsByReplica := make(chan error, 4)
	for range 4 {
		go func() {
			db, err := sql.Open("pgx", databaseURL)
			if err == nil {
				_, err = NewPostgresServiceStore(context.Background(), db)
				_ = db.Close()
			}
			errorsByReplica <- err
		}()
	}
	for range 4 {
		if err := <-errorsByReplica; err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresRowLevelTenantIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_TENANT_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_TENANT_DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The non-owner application role exercises RLS against a schema migrated by
	// the administrative bootstrap connection; it intentionally has no DDL.
	store := NewServiceStore()
	store.db = db
	suffix := time.Now().UTC().Format("20060102150405")
	alphaID, betaID := TenantID("alpha-"+suffix), TenantID("beta-"+suffix)
	adminURL := os.Getenv("TEST_DATABASE_URL")
	adminDB, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	adminStore, err := NewPostgresServiceStore(context.Background(), adminDB)
	if err != nil {
		t.Fatal(err)
	}
	if err = adminStore.EnsureTenants(context.Background(), []TenantID{alphaID, betaID}); err != nil {
		t.Fatal(err)
	}
	alpha, beta := store.ForTenant(alphaID), store.ForTenant(betaID)
	name := "shared-name"
	if err = alpha.Put(Service{TenantID: alphaID, Name: name, RepoURL: "https://github.com/acme/alpha-" + suffix, Version: 1}); err != nil {
		t.Fatal(err)
	}
	if err = beta.Put(Service{TenantID: betaID, Name: name, RepoURL: "https://github.com/acme/beta-" + suffix, Version: 1}); err != nil {
		t.Fatal(err)
	}
	if services := alpha.List(); len(services) != 1 || services[0].TenantID != alphaID {
		t.Fatalf("alpha observed cross-tenant rows: %#v", services)
	}
	if services := beta.List(); len(services) != 1 || services[0].TenantID != betaID {
		t.Fatalf("beta observed cross-tenant rows: %#v", services)
	}
	alphaOnly := &orchestrator.Environment{TenantID: string(alphaID), Spec: orchestrator.EnvironmentSpec{Name: "alpha-only", Service: name}}
	if err = alpha.PutEnvironment(alphaOnly); err != nil {
		t.Fatal(err)
	}
	if _, err = beta.GetEnvironment("alpha-only"); !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("beta crossed the environment RLS boundary: %v", err)
	}
	commandRoot := &PostgresCommandStore{db: db, tenantID: DefaultTenantID, privileged: true}
	alphaCommands := commandRoot.CommandsForTenant(alphaID)
	betaCommands := commandRoot.CommandsForTenant(betaID)
	now := time.Now().UTC()
	for tenantID, commands := range map[TenantID]SCMCommandStore{alphaID: alphaCommands, betaID: betaCommands} {
		command := &scm.LifecycleCommand{TenantID: string(tenantID), ID: "shared-command", Provider: scm.ProviderGitHub, DeliveryID: "shared-delivery", Type: scm.EnsurePreviewEnvironment, Repository: "acme/shared", PullRequest: 1, Environment: "shared-preview", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
		if duplicate, recordErr := commands.RecordSCMDelivery(scm.ProviderGitHub, "shared-delivery", command, now); recordErr != nil || duplicate {
			t.Fatalf("tenant %s could not record namespaced delivery: duplicate=%v err=%v", tenantID, duplicate, recordErr)
		}
	}
	if duplicate, recordErr := alphaCommands.RecordSCMDelivery(scm.ProviderGitHub, "shared-delivery", nil, now); recordErr != nil || !duplicate {
		t.Fatalf("alpha delivery replay escaped tenant deduplication: duplicate=%v err=%v", duplicate, recordErr)
	}
	if commands := alphaCommands.SCMCommands(); len(commands) != 1 || commands[0].TenantID != string(alphaID) {
		t.Fatalf("alpha observed cross-tenant commands: %#v", commands)
	}
	for tenantID, commands := range map[TenantID]SCMCommandStore{alphaID: alphaCommands, betaID: betaCommands} {
		leased, leaseErr := commands.LeaseSCMCommand(now, time.Minute)
		if leaseErr != nil || leased.TenantID != string(tenantID) {
			t.Fatalf("tenant %s leased foreign command %#v: %v", tenantID, leased, leaseErr)
		}
		if err = commands.CompleteSCMCommand(leased.ID, nil, now); err != nil {
			t.Fatal(err)
		}
	}

	// A raw transaction intentionally omits tenant_id predicates. RLS itself,
	// not merely repository filtering, must reduce visibility to the bound scope.
	tx, err := beginTenantTx(context.Background(), db, alphaID, false)
	if err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = tx.QueryRow(`SELECT count(*) FROM services WHERE name=$1`, name).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Fatalf("RLS exposed %d same-name services to alpha", visible)
	}
	if _, err = tx.Exec(`INSERT INTO services(tenant_id,name,document,version) VALUES($1,$2,'{}',1)`, betaID, "cross-scope-write"); err == nil {
		t.Fatal("RLS allowed alpha to insert a beta-owned service")
	}
	_ = tx.Rollback()
}
