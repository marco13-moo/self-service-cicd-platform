package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

var ErrEnvironmentNotFound = errors.New("environment not found")
var ErrServiceNotFound = errors.New("service not found")

// ServiceStore is a concurrency-safe repository for control-plane intent and
// immutable workflow references. When path is non-empty, mutations are durable.
type ServiceStore struct {
	mu               sync.RWMutex
	path             string
	services         map[string]Service
	environments     map[string]*orchestrator.Environment
	githubDeliveries map[string]time.Time
	githubCommands   []GitHubWebhookCommand
}

type persistedState struct {
	Services         map[string]Service                   `json:"services"`
	Environments     map[string]*orchestrator.Environment `json:"environments"`
	GitHubDeliveries map[string]time.Time                 `json:"github_deliveries,omitempty"`
	GitHubCommands   []GitHubWebhookCommand               `json:"github_commands,omitempty"`
}

func NewServiceStore() *ServiceStore {
	return &ServiceStore{
		services:         make(map[string]Service),
		environments:     make(map[string]*orchestrator.Environment),
		githubDeliveries: make(map[string]time.Time),
		githubCommands:   make([]GitHubWebhookCommand, 0),
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
	}
	if state.Environments != nil {
		store.environments = state.Environments
	}
	if state.GitHubDeliveries != nil {
		store.githubDeliveries = state.GitHubDeliveries
	}
	if state.GitHubCommands != nil {
		store.githubCommands = state.GitHubCommands
	}
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

func (s *ServiceStore) PutEnvironment(env *orchestrator.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.environments[env.Spec.Name]
	s.environments[env.Spec.Name] = env
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
	return env, nil
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

// RecordGitHubDelivery atomically establishes delivery idempotency and appends
// a durable lifecycle command. A nil command records an accepted event that
// intentionally has no downstream side effect.
func (s *ServiceStore) RecordGitHubDelivery(deliveryID string, command *GitHubWebhookCommand, receivedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.githubDeliveries[deliveryID]; exists {
		return true, nil
	}
	previousDeliveries := make(map[string]time.Time, len(s.githubDeliveries))
	for id, timestamp := range s.githubDeliveries {
		previousDeliveries[id] = timestamp
	}
	previousCommandCount := len(s.githubCommands)

	// GitHub delivery IDs are retained long enough to absorb realistic retries
	// without allowing the single-writer state file to grow indefinitely.
	cutoff := receivedAt.Add(-7 * 24 * time.Hour)
	for id, timestamp := range s.githubDeliveries {
		if timestamp.Before(cutoff) {
			delete(s.githubDeliveries, id)
		}
	}
	s.githubDeliveries[deliveryID] = receivedAt
	if command != nil {
		s.githubCommands = append(s.githubCommands, *command)
	}
	if err := s.persistLocked(); err != nil {
		s.githubDeliveries = previousDeliveries
		s.githubCommands = s.githubCommands[:previousCommandCount]
		return false, err
	}
	return false, nil
}

func (s *ServiceStore) GitHubCommands() []GitHubWebhookCommand {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]GitHubWebhookCommand, len(s.githubCommands))
	copy(out, s.githubCommands)
	return out
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
		Services:         s.services,
		Environments:     s.environments,
		GitHubDeliveries: s.githubDeliveries,
		GitHubCommands:   s.githubCommands,
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
