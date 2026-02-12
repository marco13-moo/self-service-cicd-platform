/*
package api

import "sync"

// ServiceStore is an in-memory control-plane registry.
// This will later be replaced by a persistent backend.

	type ServiceStore struct {
		mu       sync.RWMutex
		services []Service
	}

	func NewServiceStore() *ServiceStore {
		return &ServiceStore{
			services: make([]Service, 0),
		}
	}

	func (s *ServiceStore) Add(service Service) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.services = append(s.services, service)
	}

	func (s *ServiceStore) List() []Service {
		s.mu.RLock()
		defer s.mu.RUnlock()

		out := make([]Service, len(s.services))
		copy(out, s.services)
		return out
	}
*/
package api

import (
	"errors"
	"sync"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

var ErrEnvironmentNotFound = errors.New("environment not found")
var ErrServiceNotFound = errors.New("service not found")

type ServiceStore struct {
	mu sync.RWMutex

	services     map[string]Service
	environments map[string]*orchestrator.Environment
}

func NewServiceStore() *ServiceStore {
	return &ServiceStore{
		services:     make(map[string]Service),
		environments: make(map[string]*orchestrator.Environment),
	}
}

//
// -----------------------------
// Service Methods
// -----------------------------

func (s *ServiceStore) Put(service Service) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.services[service.Name] = service
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

	return out
}

//
// -----------------------------
// Environment Methods
// -----------------------------

func (s *ServiceStore) PutEnvironment(env *orchestrator.Environment) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.environments[env.Spec.Name] = env
}

func (s *ServiceStore) GetEnvironment(
	name string,
) (*orchestrator.Environment, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	env, ok := s.environments[name]
	if !ok {
		return nil, ErrEnvironmentNotFound
	}

	return env, nil
}

func (s *ServiceStore) DeleteEnvironment(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.environments, name)
}
