package orchestrator

import (
	"context"
	"time"
)

type EnvironmentSpec struct {
	Name       string
	Service    string
	TTL        time.Duration
	Parameters map[string]string
}

type EnvironmentOrchestrator interface {
	Create(ctx context.Context, spec EnvironmentSpec) error
	Destroy(ctx context.Context, name string) error
}
