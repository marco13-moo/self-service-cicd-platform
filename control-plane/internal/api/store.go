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
