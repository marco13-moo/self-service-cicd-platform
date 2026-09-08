// Command state-migrate transfers the legacy JSON desired-state repository to
// an empty PostgreSQL database. It is deliberately separate from server startup
// so concurrent replicas cannot race an implicit data migration.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
)

func main() {
	statePath := flag.String("state-path", "/var/lib/control-plane/state.json", "legacy JSON state file")
	flag.Parse()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	services, environments, err := api.ImportPersistentState(context.Background(), db, *statePath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("imported %d services and %d environments\n", services, environments)
}
