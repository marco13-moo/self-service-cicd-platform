package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

var ErrEnvironmentNotFound = errors.New("environment not found")
var ErrServiceNotFound = errors.New("service not found")
var ErrCommandNotFound = errors.New("SCM command not found")
var ErrVersionConflict = errors.New("state version conflict")
var ErrTenantScope = errors.New("tenant scope mismatch")

// ServiceStore is a concurrency-safe repository for control-plane intent and
// immutable workflow references. When path is non-empty, mutations are durable.
type ServiceStore struct {
	mu            *sync.RWMutex
	path          string
	services      map[string]Service
	environments  map[string]*orchestrator.Environment
	scmDeliveries map[string]time.Time
	scmCommands   []scm.LifecycleCommand
	db            *sql.DB
	tenantID      TenantID
}

// NewPostgresServiceStore makes PostgreSQL authoritative for service and
// environment state. File-backed maps remain unpopulated and cannot diverge.
func NewPostgresServiceStore(ctx context.Context, db *sql.DB) (*ServiceStore, error) {
	if err := migrateDatabase(ctx, db); err != nil {
		return nil, err
	}
	store := NewServiceStore()
	store.db = db
	return store, nil
}

// ImportPersistentState performs an intentionally explicit, one-time transfer
// from the legacy JSON repository into an empty PostgreSQL state plane. Refusing
// a non-empty target prevents an operator from silently merging divergent
// authoritative histories during a rolling migration.
func ImportPersistentState(ctx context.Context, db *sql.DB, path string) (int, int, error) {
	legacy, err := NewPersistentServiceStore(path)
	if err != nil {
		return 0, 0, err
	}
	target, err := NewPostgresServiceStore(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	if err = target.Ready(ctx); err != nil {
		return 0, 0, fmt.Errorf("verify PostgreSQL target: %w", err)
	}
	services := legacy.List()
	environments := legacy.ListEnvironments()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin state import: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.bypass_rls','off',true)`, string(DefaultTenantID)); err != nil {
		return 0, 0, fmt.Errorf("bind import tenant: %w", err)
	}
	// Serialize the emptiness check and import with migrations and any competing
	// importer. Normal writers cannot exist during the documented stopped-writer
	// cutover; the lock makes accidental duplicate importers deterministic.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return 0, 0, fmt.Errorf("lock state import: %w", err)
	}
	var stateRows int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM services) + (SELECT count(*) FROM environments)`).Scan(&stateRows); err != nil {
		return 0, 0, fmt.Errorf("inspect PostgreSQL target: %w", err)
	}
	if stateRows != 0 {
		return 0, 0, errors.New("PostgreSQL target already contains service or environment state")
	}
	for _, service := range services {
		service.TenantID = DefaultTenantID
		service.Version = 1
		document, marshalErr := json.Marshal(service)
		if marshalErr != nil {
			return 0, 0, fmt.Errorf("encode service %q: %w", service.Name, marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO services(name,document,version) VALUES($1,$2,1)`, service.Name, document); err != nil {
			return 0, 0, fmt.Errorf("import service %q: %w", service.Name, err)
		}
	}
	for _, environment := range environments {
		environment.TenantID = string(DefaultTenantID)
		environment.Version = 1
		document, marshalErr := json.Marshal(environment)
		if marshalErr != nil {
			return 0, 0, fmt.Errorf("encode environment %q: %w", environment.Spec.Name, marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO environments(name,document,version) VALUES($1,$2,1)`, environment.Spec.Name, document); err != nil {
			return 0, 0, fmt.Errorf("import environment %q: %w", environment.Spec.Name, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit state import: %w", err)
	}
	return len(services), len(environments), nil
}

func (s *ServiceStore) Ready(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	return s.db.PingContext(ctx)
}

func (s *ServiceStore) EnsureTenants(ctx context.Context, tenantIDs []TenantID) error {
	if s.db == nil {
		return nil
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tenantID := range tenantIDs {
		if !validTenantID(tenantID) {
			return fmt.Errorf("invalid tenant %q", tenantID)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tenants(id) VALUES($1) ON CONFLICT DO NOTHING`, tenantID); err != nil {
			return fmt.Errorf("ensure tenant %q: %w", tenantID, err)
		}
	}
	return tx.Commit()
}

// GetTenantAuthConfig reads the tenant_auths row for a tenant and returns the
// raw JSON object as-is. Returns sql.ErrNoRows if not configured.
func (s *ServiceStore) GetTenantAuthConfig(ctx context.Context, tenantID TenantID) (map[string]interface{}, error) {
	if s.db == nil {
		return nil, sql.ErrNoRows
	}
	if normalizeTenantID(s.tenantID) != normalizeTenantID(tenantID) {
		return nil, ErrTenantScope
	}
	tx, err := beginTenantTx(ctx, s.db, tenantID, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT config FROM tenant_auths WHERE tenant_id=$1`, tenantID).Scan(&raw); err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// PutTenantAuthConfig writes the tenant_auths row for a tenant. Upserts the
// JSON payload into the config column.
func (s *ServiceStore) PutTenantAuthConfig(ctx context.Context, tenantID TenantID, payload interface{}) error {
	if s.db == nil {
		return errors.New("tenant OIDC configuration requires PostgreSQL")
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if normalizeTenantID(s.tenantID) != normalizeTenantID(tenantID) {
		return ErrTenantScope
	}
	var typed TenantAuthConfigPayload
	if err := json.Unmarshal(b, &typed); err != nil {
		return err
	}
	tx, err := beginTenantTx(ctx, s.db, tenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, provider := range typed.Providers {
		var collision bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_auths WHERE tenant_id<>$1 AND config @> jsonb_build_object('providers',jsonb_build_array(jsonb_build_object('issuer',$2::text))))`, tenantID, provider.Issuer).Scan(&collision); err != nil {
			return err
		}
		if collision {
			return errors.New("OIDC issuer is already assigned to another tenant")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_auths(tenant_id,config) VALUES($1,$2) ON CONFLICT (tenant_id) DO UPDATE SET config=$2,updated_at=now()`, tenantID, b); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveTenantAuthByIssuer performs the only privileged tenant-auth lookup.
// The issuer is treated solely as a selector; cryptographic validation binds
// the returned provider and tenant before a Principal is constructed.
func (s *ServiceStore) ResolveTenantAuthByIssuer(ctx context.Context, issuer string) (TenantID, TenantAuthConfigPayload, error) {
	if s.db == nil {
		return "", TenantAuthConfigPayload{}, sql.ErrNoRows
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return "", TenantAuthConfigPayload{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id,config FROM tenant_auths WHERE config @> jsonb_build_object('providers',jsonb_build_array(jsonb_build_object('issuer',$1::text)))`, issuer)
	if err != nil {
		return "", TenantAuthConfigPayload{}, err
	}
	defer rows.Close()
	var tenantID TenantID
	var payload TenantAuthConfigPayload
	matches := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&tenantID, &raw); err != nil {
			return "", TenantAuthConfigPayload{}, err
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", TenantAuthConfigPayload{}, err
		}
		matches++
	}
	if err := rows.Err(); err != nil {
		return "", TenantAuthConfigPayload{}, err
	}
	if matches == 0 {
		return "", TenantAuthConfigPayload{}, sql.ErrNoRows
	}
	if matches != 1 {
		return "", TenantAuthConfigPayload{}, errors.New("OIDC issuer is ambiguously assigned")
	}
	return tenantID, payload, tx.Commit()
}

type AuditEvent struct {
	ID            uuid.UUID      `json:"id"`
	TenantID      TenantID       `json:"tenant_id"`
	OccurredAt    time.Time      `json:"occurred_at"`
	CorrelationID string         `json:"correlation_id"`
	Actor         string         `json:"actor"`
	EventType     string         `json:"event_type"`
	ResourceType  string         `json:"resource_type"`
	ResourceName  string         `json:"resource_name"`
	Outcome       string         `json:"outcome"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (s *ServiceStore) AppendAuditEvent(ctx context.Context, event AuditEvent) error {
	if s.db == nil {
		return nil
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	tx, err := beginTenantTx(ctx, s.db, event.TenantID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,tenant_id,occurred_at,correlation_id,actor,event_type,resource_type,resource_name,outcome,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, event.ID, event.TenantID, event.OccurredAt, event.CorrelationID, event.Actor, event.EventType, event.ResourceType, event.ResourceName, event.Outcome, metadata)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ServiceStore) ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	if s.db == nil {
		return []AuditEvent{}, nil
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	tenantID := normalizeTenantID(s.tenantID)
	tx, err := beginTenantTx(ctx, s.db, tenantID, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,tenant_id,occurred_at,correlation_id,actor,event_type,resource_type,resource_name,outcome,metadata FROM audit_events ORDER BY occurred_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]AuditEvent, 0)
	for rows.Next() {
		var event AuditEvent
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.TenantID, &event.OccurredAt, &event.CorrelationID, &event.Actor, &event.EventType, &event.ResourceType, &event.ResourceName, &event.Outcome, &metadata); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, tx.Commit()
}

func (s *ServiceStore) TenantActive(ctx context.Context, tenantID TenantID) (bool, error) {
	if s.db == nil {
		return true, nil
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=$1`, tenantID).Scan(&status); err != nil {
		return false, err
	}
	return status == "active", tx.Commit()
}

func (s *ServiceStore) SetTenantStatus(ctx context.Context, tenantID TenantID, status string) error {
	if s.db == nil {
		return errors.New("tenant lifecycle requires PostgreSQL")
	}
	if !validTenantID(tenantID) || (status != "active" && status != "suspended" && status != "offboarded") {
		return errors.New("invalid tenant lifecycle transition")
	}
	if tenantID == DefaultTenantID && status != "active" {
		return errors.New("the platform bootstrap tenant cannot be suspended or offboarded")
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=$1 FOR UPDATE`, tenantID).Scan(&currentStatus); err != nil {
		return err
	}
	if currentStatus == "offboarded" && status != "offboarded" {
		return errors.New("offboarded tenant is terminal and cannot be reactivated")
	}
	if status == "offboarded" {
		var owned int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM services WHERE tenant_id=$1)+(SELECT count(*) FROM environments WHERE tenant_id=$1)`, tenantID).Scan(&owned); err != nil {
			return err
		}
		if owned != 0 {
			return errors.New("tenant must own no services or environments before offboarding")
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE tenants SET status=$2,updated_at=now() WHERE id=$1`, tenantID, status)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *ServiceStore) ProvisionTenant(ctx context.Context, tenantID TenantID) error {
	if s.db == nil {
		return errors.New("tenant provisioning requires PostgreSQL")
	}
	if !validTenantID(tenantID) {
		return errors.New("invalid tenant identifier")
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO tenants(id,status) VALUES($1,'active') ON CONFLICT DO NOTHING`, tenantID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("tenant already exists")
	}
	return tx.Commit()
}

func (s *ServiceStore) TransferService(ctx context.Context, sourceTenant, targetTenant TenantID, serviceName string) error {
	if s.db == nil {
		return errors.New("repository transfer requires PostgreSQL")
	}
	if !validTenantID(sourceTenant) || !validTenantID(targetTenant) || sourceTenant == targetTenant || strings.TrimSpace(serviceName) == "" {
		return errors.New("invalid repository transfer")
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var targetStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=$1 FOR UPDATE`, targetTenant).Scan(&targetStatus); err != nil {
		return err
	}
	if targetStatus != "active" {
		return errors.New("target tenant is not active")
	}
	var dependent int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM environments WHERE tenant_id=$1 AND document->'spec'->>'service'=$2`, sourceTenant, serviceName).Scan(&dependent); err != nil {
		return err
	}
	if dependent != 0 {
		return errors.New("service transfer requires all preview environments to be destroyed")
	}
	result, err := tx.ExecContext(ctx, `UPDATE services SET tenant_id=$3,document=jsonb_set(document,'{tenant_id}',to_jsonb($3::text),true),version=version+1,updated_at=now() WHERE tenant_id=$1 AND name=$2`, sourceTenant, serviceName, targetTenant)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrServiceNotFound
	}
	return tx.Commit()
}

func (s *ServiceStore) AppendPlatformAuditEvent(ctx context.Context, event AuditEvent) error {
	if s.db == nil {
		return nil
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	tx, err := beginTenantTx(ctx, s.db, DefaultTenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,tenant_id,occurred_at,correlation_id,actor,event_type,resource_type,resource_name,outcome,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, event.ID, event.TenantID, event.OccurredAt, event.CorrelationID, event.Actor, event.EventType, event.ResourceType, event.ResourceName, event.Outcome, metadata)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type persistedState struct {
	Services               map[string]Service                   `json:"services"`
	Environments           map[string]*orchestrator.Environment `json:"environments"`
	SCMDeliveries          map[string]time.Time                 `json:"scm_deliveries,omitempty"`
	SCMCommands            []scm.LifecycleCommand               `json:"scm_commands,omitempty"`
	LegacyGitHubDeliveries map[string]time.Time                 `json:"github_deliveries,omitempty"`
	LegacyGitHubCommands   []legacyGitHubCommand                `json:"github_commands,omitempty"`
}

type legacyGitHubCommand struct {
	DeliveryID     string    `json:"delivery_id"`
	Type           string    `json:"type"`
	Repository     string    `json:"repository"`
	InstallationID int64     `json:"installation_id"`
	PullRequest    int       `json:"pull_request"`
	HeadSHA        string    `json:"head_sha"`
	Environment    string    `json:"environment"`
	ReceivedAt     time.Time `json:"received_at"`
}

func NewServiceStore() *ServiceStore {
	return &ServiceStore{
		mu:            &sync.RWMutex{},
		services:      make(map[string]Service),
		environments:  make(map[string]*orchestrator.Environment),
		scmDeliveries: make(map[string]time.Time),
		scmCommands:   make([]scm.LifecycleCommand, 0),
		tenantID:      DefaultTenantID,
	}
}

// ForTenant returns a lightweight repository capability constrained to one
// tenant while sharing the underlying pool or concurrency-safe file state.
func (s *ServiceStore) ForTenant(tenantID TenantID) *ServiceStore {
	tenantID = normalizeTenantID(tenantID)
	if !validTenantID(tenantID) {
		panic("invalid tenant repository scope")
	}
	return &ServiceStore{mu: s.mu, path: s.path, services: s.services, environments: s.environments, scmDeliveries: s.scmDeliveries, scmCommands: s.scmCommands, db: s.db, tenantID: tenantID}
}

func (s *ServiceStore) stateKey(name string) string {
	if s.tenantID == "" || s.tenantID == DefaultTenantID {
		return name
	}
	return string(s.tenantID) + "\x1f" + name
}

// NewPersistentServiceStore loads existing state. Malformed state is rejected
// explicitly, preventing a silent reset of control-plane ownership metadata.
func NewPersistentServiceStore(path string) (*ServiceStore, error) {
	store := NewServiceStore()
	store.path = path
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	if state.Services != nil {
		store.services = state.Services
		for name, service := range store.services {
			if service.Repository.Provider == "" {
				if identity, parseErr := scm.ParseRepositoryIdentity(service.RepoURL); parseErr == nil {
					service.Repository = identity
					store.services[name] = service
				}
			}
		}
	}
	if state.Environments != nil {
		store.environments = state.Environments
	}
	if state.SCMDeliveries != nil {
		store.scmDeliveries = state.SCMDeliveries
	}
	if state.SCMCommands != nil {
		store.scmCommands = state.SCMCommands
	}
	store.migrateLegacyGitHubState(state)
	return store, nil
}

func (s *ServiceStore) Put(service Service) error {
	if s.db != nil {
		return s.putServicePostgres(service)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	service.TenantID = s.tenantID
	key := s.stateKey(service.Name)
	previous, existed := s.services[key]
	s.services[key] = service
	if err := s.persistLocked(); err != nil {
		if existed {
			s.services[service.Name] = previous
		} else {
			delete(s.services, key)
		}
		return err
	}
	return nil
}

func (s *ServiceStore) Get(name string) (Service, error) {
	if s.db != nil {
		return s.getServicePostgres(name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[s.stateKey(name)]
	if !ok {
		return Service{}, ErrServiceNotFound
	}
	return svc, nil
}

func (s *ServiceStore) List() []Service {
	if s.db != nil {
		return s.listServicesPostgres()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Service, 0, len(s.services))
	for _, svc := range s.services {
		if normalizeTenantID(svc.TenantID) != normalizeTenantID(s.tenantID) {
			continue
		}
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Delete removes a service only after every preview has been destroyed. This
// makes tenant offboarding explicit without permitting orphaned workloads.
func (s *ServiceStore) Delete(name string) error {
	if s.db != nil {
		tx, err := beginTenantTx(context.Background(), s.db, s.tenantID, false)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var environments int
		if err = tx.QueryRow(`SELECT count(*) FROM environments WHERE tenant_id=$1 AND document->'spec'->>'service'=$2`, s.tenantID, name).Scan(&environments); err != nil {
			return err
		}
		if environments != 0 {
			return fmt.Errorf("service has preview environments")
		}
		result, err := tx.Exec(`DELETE FROM services WHERE tenant_id=$1 AND name=$2`, s.tenantID, name)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrServiceNotFound
		}
		return tx.Commit()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, environment := range s.environments {
		if normalizeTenantID(TenantID(environment.TenantID)) == normalizeTenantID(s.tenantID) && environment.Spec.Service == name {
			return fmt.Errorf("service has preview environments")
		}
	}
	key := s.stateKey(name)
	if _, ok := s.services[key]; !ok {
		return ErrServiceNotFound
	}
	previous := s.services[key]
	delete(s.services, key)
	if err := s.persistLocked(); err != nil {
		s.services[key] = previous
		return err
	}
	return nil
}

func (s *ServiceStore) FindServiceByRepository(repository string) (Service, error) {
	if s.db != nil {
		for _, service := range s.listServicesPostgres() {
			if serviceMatchesRepository(service, repository) {
				return service, nil
			}
		}
		return Service{}, ErrServiceNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, service := range s.services {
		if normalizeTenantID(service.TenantID) != normalizeTenantID(s.tenantID) {
			continue
		}
		if serviceMatchesRepository(service, repository) {
			return service, nil
		}
	}
	return Service{}, ErrServiceNotFound
}

// ResolveTenantForRepository is the narrow ingress lookup used after a webhook
// signature has been verified. Repository ownership is globally unique, so the
// lookup reveals only the tenant capability required to persist that delivery.
func (s *ServiceStore) ResolveTenantForRepository(repository string) (TenantID, error) {
	if s.db != nil {
		tx, err := beginTenantTx(context.Background(), s.db, DefaultTenantID, true)
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		rows, err := tx.Query(`SELECT tenant_id,document FROM services`)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		for rows.Next() {
			var tenantID TenantID
			var document []byte
			var service Service
			if err = rows.Scan(&tenantID, &document); err != nil {
				return "", err
			}
			if json.Unmarshal(document, &service) == nil && serviceMatchesRepository(service, repository) {
				return tenantID, nil
			}
		}
		return "", ErrServiceNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, service := range s.services {
		if serviceMatchesRepository(service, repository) {
			return normalizeTenantID(service.TenantID), nil
		}
	}
	return "", ErrServiceNotFound
}

func serviceMatchesRepository(service Service, repository string) bool {
	wanted := strings.ToLower(strings.TrimSuffix(strings.Trim(repository, "/"), ".git"))
	if service.Repository.Provider != "" && strings.EqualFold(service.Repository.Workspace+"/"+service.Repository.Name, wanted) {
		return true
	}
	candidate := strings.ToLower(strings.TrimSuffix(strings.Trim(service.RepoURL, "/"), ".git"))
	return candidate == wanted || strings.HasSuffix(candidate, "/"+wanted) || strings.HasSuffix(candidate, ":"+wanted)
}

func (s *ServiceStore) PutEnvironment(env *orchestrator.Environment) error {
	if s.db != nil {
		return s.putEnvironmentPostgres(env)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	env.TenantID = string(s.tenantID)
	key := s.stateKey(env.Spec.Name)
	previous, existed := s.environments[key]
	if existed && env.Version != previous.Version {
		return ErrVersionConflict
	}
	if !existed && env.Version != 0 {
		return ErrVersionConflict
	}
	env.Version++
	s.environments[key] = cloneEnvironment(env)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.environments[key] = previous
		} else {
			delete(s.environments, key)
		}
		env.Version--
		return err
	}
	return nil
}

func (s *ServiceStore) GetEnvironment(name string) (*orchestrator.Environment, error) {
	if s.db != nil {
		return s.getEnvironmentPostgres(name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	env, ok := s.environments[s.stateKey(name)]
	if !ok {
		return nil, ErrEnvironmentNotFound
	}
	return cloneEnvironment(env), nil
}

// ListEnvironments returns detached snapshots so observers cannot mutate the
// authoritative store without passing through an atomic persistence boundary.
func (s *ServiceStore) ListEnvironments() []*orchestrator.Environment {
	if s.db != nil {
		return s.listEnvironmentsPostgres()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.environments))
	for key, environment := range s.environments {
		if normalizeTenantID(TenantID(environment.TenantID)) == normalizeTenantID(s.tenantID) {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	out := make([]*orchestrator.Environment, 0, len(names))
	for _, name := range names {
		out = append(out, cloneEnvironment(s.environments[name]))
	}
	return out
}

// ListAllEnvironments is restricted to the internal reconciler capability; API
// handlers must always use ListEnvironments on a tenant-scoped repository.
func (s *ServiceStore) ListAllEnvironments() []*orchestrator.Environment {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := make([]*orchestrator.Environment, 0, len(s.environments))
		for _, environment := range s.environments {
			out = append(out, cloneEnvironment(environment))
		}
		return out
	}
	tx, err := beginTenantTx(context.Background(), s.db, DefaultTenantID, true)
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT document FROM environments ORDER BY tenant_id,name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var environments []*orchestrator.Environment
	for rows.Next() {
		var document []byte
		var environment orchestrator.Environment
		if rows.Scan(&document) == nil && json.Unmarshal(document, &environment) == nil {
			environments = append(environments, &environment)
		}
	}
	return environments
}

type DeploymentEvidence struct {
	ImageDigest         string
	DeployedImage       string
	SBOMReference       string
	ProvenanceReference string
	VulnerabilityPolicy string
	SignatureReference  string
	PolicyAttestation   string
}

// ObserveDeployment performs a generation-aware compare-and-set. A terminal
// result from an obsolete Workflow is ignored rather than being allowed to
// promote the desired artifact of a newer deployment generation.
func (s *ServiceStore) ObserveDeployment(name, workflowName string, generation int64, phase, message string, observedAt time.Time, evidence DeploymentEvidence) (bool, error) {
	if s.db != nil {
		return s.observeDeploymentPostgres(name, workflowName, generation, phase, message, observedAt, evidence)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.stateKey(name)
	env, ok := s.environments[key]
	if !ok {
		return false, ErrEnvironmentNotFound
	}
	if env.Spec.Source == nil || env.DeployWorkflow == nil || env.DeployWorkflow.Name != workflowName || env.Spec.Source.Generation != generation {
		return false, nil
	}
	source := env.Spec.Source
	if source.DeploymentPhase == phase && source.DeploymentMessage == message && !(phase == "Succeeded" && (source.DeployedSHA != source.DesiredSHA || source.DeployedImage != evidence.DeployedImage || source.PreviewURL != source.DesiredPreviewURL)) {
		return false, nil
	}
	previous := cloneEnvironment(env)
	env.Version++
	stamp := observedAt.UTC()
	source.DeploymentPhase = phase
	source.DeploymentMessage = message
	source.ObservedAt = &stamp
	if phase == "Succeeded" {
		source.DeployedSHA = source.DesiredSHA
		source.DeployedImage = evidence.DeployedImage
		source.ImageDigest = evidence.ImageDigest
		source.SBOMReference = evidence.SBOMReference
		source.ProvenanceReference = evidence.ProvenanceReference
		source.VulnerabilityPolicy = evidence.VulnerabilityPolicy
		source.SignatureReference = evidence.SignatureReference
		source.PolicyAttestation = evidence.PolicyAttestation
		source.PreviewURL = source.DesiredPreviewURL
	}
	if err := s.persistLocked(); err != nil {
		s.environments[key] = previous
		return false, err
	}
	return true, nil
}

func cloneEnvironment(env *orchestrator.Environment) *orchestrator.Environment {
	if env == nil {
		return nil
	}
	clone := *env
	clone.Spec = env.Spec
	if env.Spec.Parameters != nil {
		clone.Spec.Parameters = make(map[string]string, len(env.Spec.Parameters))
		for key, value := range env.Spec.Parameters {
			clone.Spec.Parameters[key] = value
		}
	}
	if env.Spec.Source != nil {
		source := *env.Spec.Source
		if env.Spec.Source.ObservedAt != nil {
			observedAt := *env.Spec.Source.ObservedAt
			source.ObservedAt = &observedAt
		}
		clone.Spec.Source = &source
	}
	if env.DestroyWorkflow != nil {
		ref := *env.DestroyWorkflow
		clone.DestroyWorkflow = &ref
	}
	if env.TTLWorkflow != nil {
		ref := *env.TTLWorkflow
		clone.TTLWorkflow = &ref
	}
	if env.DeployWorkflow != nil {
		ref := *env.DeployWorkflow
		clone.DeployWorkflow = &ref
	}
	return &clone
}

func (s *ServiceStore) DeleteEnvironment(name string) error {
	if s.db != nil {
		tx, err := beginTenantTx(context.Background(), s.db, s.tenantID, false)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec(`DELETE FROM environments WHERE tenant_id=$1 AND name=$2`, s.tenantID, name)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return ErrEnvironmentNotFound
		}
		return tx.Commit()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.stateKey(name)
	previous, existed := s.environments[key]
	if !existed {
		return ErrEnvironmentNotFound
	}
	delete(s.environments, key)
	if err := s.persistLocked(); err != nil {
		s.environments[key] = previous
		return err
	}
	return nil
}

// RecordSCMDelivery atomically establishes provider-scoped delivery idempotency and appends
// a durable lifecycle command. A nil command records an accepted event that
// intentionally has no downstream side effect.
func (s *ServiceStore) RecordSCMDelivery(provider scm.Provider, deliveryID string, command *scm.LifecycleCommand, receivedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scm.DeliveryKey(provider, deliveryID)
	if _, exists := s.scmDeliveries[key]; exists {
		return true, nil
	}
	previousDeliveries := make(map[string]time.Time, len(s.scmDeliveries))
	for id, timestamp := range s.scmDeliveries {
		previousDeliveries[id] = timestamp
	}
	previousCommands := make([]scm.LifecycleCommand, len(s.scmCommands))
	copy(previousCommands, s.scmCommands)

	// GitHub delivery IDs are retained long enough to absorb realistic retries
	// without allowing the single-writer state file to grow indefinitely.
	cutoff := receivedAt.Add(-7 * 24 * time.Hour)
	for id, timestamp := range s.scmDeliveries {
		if timestamp.Before(cutoff) {
			delete(s.scmDeliveries, id)
		}
	}
	s.scmDeliveries[key] = receivedAt
	if command != nil {
		for index := range s.scmCommands {
			existing := &s.scmCommands[index]
			if existing.Environment == command.Environment && (existing.Status == scm.CommandPending || existing.Status == scm.CommandFailed) {
				existing.Status = scm.CommandSuperseded
			}
		}
		s.scmCommands = append(s.scmCommands, *command)
	}
	if err := s.persistLocked(); err != nil {
		s.scmDeliveries = previousDeliveries
		s.scmCommands = previousCommands
		return false, err
	}
	return false, nil
}

func (s *ServiceStore) SCMCommands() []scm.LifecycleCommand {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]scm.LifecycleCommand, len(s.scmCommands))
	copy(out, s.scmCommands)
	return out
}

func (s *ServiceStore) LeaseSCMCommand(now time.Time, leaseDuration time.Duration) (*scm.LifecycleCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.scmCommands {
		command := &s.scmCommands[index]
		leaseExpired := command.Status == scm.CommandLeased && command.LeaseUntil != nil && !command.LeaseUntil.After(now)
		eligible := (command.Status == scm.CommandPending || command.Status == scm.CommandFailed || leaseExpired) && !command.AvailableAt.After(now)
		if !eligible {
			continue
		}
		previous := *command
		leaseUntil := now.Add(leaseDuration)
		command.Status, command.LeaseUntil, command.Attempts, command.LastError = scm.CommandLeased, &leaseUntil, command.Attempts+1, ""
		if err := s.persistLocked(); err != nil {
			*command = previous
			return nil, err
		}
		leased := *command
		return &leased, nil
	}
	return nil, ErrCommandNotFound
}

func (s *ServiceStore) CompleteSCMCommand(id string, processingErr error, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.scmCommands {
		command := &s.scmCommands[index]
		if command.ID != id {
			continue
		}
		previous := *command
		command.LeaseUntil = nil
		if processingErr == nil {
			command.Status, command.LastError = scm.CommandSucceeded, ""
		} else {
			command.LastError = processingErr.Error()
			if command.Attempts >= 5 {
				command.Status = scm.CommandDeadLetter
			} else {
				command.Status = scm.CommandFailed
				backoff := time.Duration(1<<min(command.Attempts, 6)) * time.Second
				command.AvailableAt = now.Add(backoff)
			}
		}
		if err := s.persistLocked(); err != nil {
			*command = previous
			return err
		}
		return nil
	}
	return ErrCommandNotFound
}

func (s *ServiceStore) migrateLegacyGitHubState(state persistedState) {
	for id, timestamp := range state.LegacyGitHubDeliveries {
		s.scmDeliveries[scm.DeliveryKey(scm.ProviderGitHub, id)] = timestamp
	}
	for _, legacy := range state.LegacyGitHubCommands {
		commandType := scm.EnsurePreviewEnvironment
		if legacy.Type == "destroy_preview_environment" {
			commandType = scm.DestroyPreviewEnvironment
		}
		s.scmCommands = append(s.scmCommands, scm.LifecycleCommand{
			ID: scm.DeliveryKey(scm.ProviderGitHub, legacy.DeliveryID), Provider: scm.ProviderGitHub, DeliveryID: legacy.DeliveryID,
			Type: commandType, Repository: legacy.Repository, InstallationID: fmt.Sprint(legacy.InstallationID), PullRequest: legacy.PullRequest,
			HeadSHA: legacy.HeadSHA, Environment: legacy.Environment, Status: scm.CommandPending, AvailableAt: legacy.ReceivedAt, CreatedAt: legacy.ReceivedAt,
		})
	}
}

// persistLocked performs a crash-safe same-directory temporary write followed
// by an atomic rename. The caller retains the write lock throughout.
func (s *ServiceStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(persistedState{
		Services:      s.services,
		Environments:  s.environments,
		SCMDeliveries: s.scmDeliveries,
		SCMCommands:   s.scmCommands,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".control-plane-state-*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("commit state: %w", err)
	}
	return nil
}
