package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

type SCMCommandStore interface {
	RecordSCMDelivery(scm.Provider, string, *scm.LifecycleCommand, time.Time) (bool, error)
	SCMCommands() []scm.LifecycleCommand
	LeaseSCMCommand(time.Time, time.Duration) (*scm.LifecycleCommand, error)
	CompleteSCMCommand(string, error, time.Time) error
}

// PostgresCommandStore provides transactional delivery deduplication and
// distributed command leasing. Service/environment desired state remains behind
// its independent repository boundary.
type PostgresCommandStore struct{ db *sql.DB }

func NewPostgresCommandStore(ctx context.Context, db *sql.DB) (*PostgresCommandStore, error) {
	store := &PostgresCommandStore{db: db}
	if _, err := db.ExecContext(ctx, postgresSchema); err != nil {
		return nil, fmt.Errorf("migrate PostgreSQL command store: %w", err)
	}
	return store, nil
}

func (s *PostgresCommandStore) RecordSCMDelivery(provider scm.Provider, deliveryID string, command *scm.LifecycleCommand, receivedAt time.Time) (bool, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO scm_deliveries(provider,delivery_id,received_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, provider, deliveryID, receivedAt)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return true, nil
	}
	if command != nil {
		_, err = tx.Exec(`UPDATE scm_commands SET status='superseded',lease_until=NULL WHERE environment=$1 AND status IN ('pending','failed')`, command.Environment)
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(`INSERT INTO scm_commands(id,provider,delivery_id,type,repository,installation_id,pull_request,head_sha,environment,status,attempts,available_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, command.ID, command.Provider, command.DeliveryID, command.Type, command.Repository, command.InstallationID, command.PullRequest, command.HeadSHA, command.Environment, command.Status, command.Attempts, command.AvailableAt, command.CreatedAt)
		if err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}

func (s *PostgresCommandStore) SCMCommands() []scm.LifecycleCommand {
	rows, err := s.db.Query(`SELECT id,provider,delivery_id,type,repository,installation_id,pull_request,head_sha,environment,status,attempts,available_at,lease_until,last_error,created_at FROM scm_commands ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []scm.LifecycleCommand
	for rows.Next() {
		var c scm.LifecycleCommand
		if err := rows.Scan(&c.ID, &c.Provider, &c.DeliveryID, &c.Type, &c.Repository, &c.InstallationID, &c.PullRequest, &c.HeadSHA, &c.Environment, &c.Status, &c.Attempts, &c.AvailableAt, &c.LeaseUntil, &c.LastError, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func (s *PostgresCommandStore) LeaseSCMCommand(now time.Time, duration time.Duration) (*scm.LifecycleCommand, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var c scm.LifecycleCommand
	err = tx.QueryRow(`SELECT c.id,c.provider,c.delivery_id,c.type,c.repository,c.installation_id,c.pull_request,c.head_sha,c.environment,c.status,c.attempts,c.available_at,c.lease_until,c.last_error,c.created_at FROM scm_commands c WHERE c.available_at<=$1 AND (c.status IN ('pending','failed') OR (c.status='leased' AND c.lease_until<=$1)) AND NOT EXISTS (SELECT 1 FROM scm_commands active WHERE active.environment=c.environment AND active.status='leased' AND active.lease_until>$1) ORDER BY c.created_at FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(&c.ID, &c.Provider, &c.DeliveryID, &c.Type, &c.Repository, &c.InstallationID, &c.PullRequest, &c.HeadSHA, &c.Environment, &c.Status, &c.Attempts, &c.AvailableAt, &c.LeaseUntil, &c.LastError, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCommandNotFound
	}
	if err != nil {
		return nil, err
	}
	lease := now.Add(duration)
	c.Status = scm.CommandLeased
	c.LeaseUntil = &lease
	c.Attempts++
	if _, err = tx.Exec(`UPDATE scm_commands SET status=$2,lease_until=$3,attempts=$4,last_error='' WHERE id=$1`, c.ID, c.Status, lease, c.Attempts); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *PostgresCommandStore) CompleteSCMCommand(id string, processingErr error, now time.Time) error {
	status := scm.CommandSucceeded
	lastError := ""
	available := now
	if processingErr != nil {
		lastError = processingErr.Error()
		var attempts int
		if err := s.db.QueryRow(`SELECT attempts FROM scm_commands WHERE id=$1`, id).Scan(&attempts); err != nil {
			return err
		}
		if attempts >= 5 {
			status = scm.CommandDeadLetter
		} else {
			status = scm.CommandFailed
			available = now.Add(time.Duration(1<<min(attempts, 6)) * time.Second)
		}
	}
	result, err := s.db.Exec(`UPDATE scm_commands SET status=$2,lease_until=NULL,last_error=$3,available_at=$4 WHERE id=$1`, id, status, lastError, available)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrCommandNotFound
	}
	return nil
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS scm_deliveries(provider TEXT NOT NULL,delivery_id TEXT NOT NULL,received_at TIMESTAMPTZ NOT NULL,PRIMARY KEY(provider,delivery_id));
CREATE TABLE IF NOT EXISTS scm_commands(id TEXT PRIMARY KEY,provider TEXT NOT NULL,delivery_id TEXT NOT NULL,type TEXT NOT NULL,repository TEXT NOT NULL,installation_id TEXT NOT NULL DEFAULT '',pull_request INTEGER NOT NULL,head_sha TEXT NOT NULL DEFAULT '',environment TEXT NOT NULL,status TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,available_at TIMESTAMPTZ NOT NULL,lease_until TIMESTAMPTZ,last_error TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL);
CREATE INDEX IF NOT EXISTS scm_commands_lease_idx ON scm_commands(status,available_at,created_at);`
