package api

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

func TestPostgresCommandStoreLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewPostgresCommandStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	delivery := "test-" + now.Format("20060102150405.000000000")
	command := &scm.LifecycleCommand{ID: "github:" + delivery, Provider: scm.ProviderGitHub, DeliveryID: delivery, Type: scm.EnsurePreviewEnvironment, Repository: "test/repo", PullRequest: 1, HeadSHA: "abc", Environment: "repo-pr-1", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if duplicate, err := store.RecordSCMDelivery(scm.ProviderGitHub, delivery, command, now); err != nil || duplicate {
		t.Fatalf("record duplicate=%v err=%v", duplicate, err)
	}
	leased, err := store.LeaseSCMCommand(now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.ID != command.ID {
		t.Fatalf("leased %q", leased.ID)
	}
	if err := store.CompleteSCMCommand(command.ID, nil, now); err != nil {
		t.Fatal(err)
	}
}
