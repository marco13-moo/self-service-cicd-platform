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
