package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

var ErrEnvironmentNotFound = errors.New("environment not found")
var ErrServiceNotFound = errors.New("service not found")
var ErrCommandNotFound = errors.New("SCM command not found")

// ServiceStore is a concurrency-safe repository for control-plane intent and
// immutable workflow references. When path is non-empty, mutations are durable.
type ServiceStore struct {
	mu            sync.RWMutex
	path          string
	services      map[string]Service
	environments  map[string]*orchestrator.Environment
	scmDeliveries map[string]time.Time
	scmCommands   []scm.LifecycleCommand
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
		services:      make(map[string]Service),
		environments:  make(map[string]*orchestrator.Environment),
		scmDeliveries: make(map[string]time.Time),
		scmCommands:   make([]scm.LifecycleCommand, 0),
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.services[service.Name]
	s.services[service.Name] = service
	if err := s.persistLocked(); err != nil {
		if existed {
			s.services[service.Name] = previous
		} else {
			delete(s.services, service.Name)
		}
		return err
	}
	return nil
}

func (s *ServiceStore) Get(name string) (Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	if !ok {
		return Service{}, ErrServiceNotFound
	}
	return svc, nil
}

func (s *ServiceStore) List() []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Service, 0, len(s.services))
	for _, svc := range s.services {
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *ServiceStore) FindServiceByRepository(repository string) (Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wanted := strings.ToLower(strings.TrimSuffix(strings.Trim(repository, "/"), ".git"))
	for _, service := range s.services {
		if service.Repository.Provider != "" && strings.EqualFold(service.Repository.Workspace+"/"+service.Repository.Name, wanted) {
			return service, nil
		}
		candidate := strings.ToLower(strings.TrimSuffix(strings.Trim(service.RepoURL, "/"), ".git"))
		if candidate == wanted || strings.HasSuffix(candidate, "/"+wanted) || strings.HasSuffix(candidate, ":"+wanted) {
			return service, nil
		}
	}
	return Service{}, ErrServiceNotFound
}

func (s *ServiceStore) PutEnvironment(env *orchestrator.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.environments[env.Spec.Name]
	s.environments[env.Spec.Name] = cloneEnvironment(env)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.environments[env.Spec.Name] = previous
		} else {
			delete(s.environments, env.Spec.Name)
		}
		return err
	}
	return nil
}

func (s *ServiceStore) GetEnvironment(name string) (*orchestrator.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	env, ok := s.environments[name]
	if !ok {
		return nil, ErrEnvironmentNotFound
	}
	return cloneEnvironment(env), nil
}

// ListEnvironments returns detached snapshots so observers cannot mutate the
// authoritative store without passing through an atomic persistence boundary.
func (s *ServiceStore) ListEnvironments() []*orchestrator.Environment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.environments))
	for name := range s.environments {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*orchestrator.Environment, 0, len(names))
	for _, name := range names {
		out = append(out, cloneEnvironment(s.environments[name]))
	}
	return out
}

type DeploymentEvidence struct {
	ImageDigest         string
	DeployedImage       string
	SBOMReference       string
	ProvenanceReference string
	VulnerabilityPolicy string
}

// ObserveDeployment performs a generation-aware compare-and-set. A terminal
// result from an obsolete Workflow is ignored rather than being allowed to
// promote the desired artifact of a newer deployment generation.
func (s *ServiceStore) ObserveDeployment(name, workflowName string, generation int64, phase, message string, observedAt time.Time, evidence DeploymentEvidence) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.environments[name]
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
		source.PreviewURL = source.DesiredPreviewURL
	}
	if err := s.persistLocked(); err != nil {
		s.environments[name] = previous
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
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.environments[name]
	if !existed {
		return ErrEnvironmentNotFound
	}
	delete(s.environments, name)
	if err := s.persistLocked(); err != nil {
		s.environments[name] = previous
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
