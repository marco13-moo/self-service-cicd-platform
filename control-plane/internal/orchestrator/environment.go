package orchestrator

import (
	"context"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

//
// ----- DOMAIN TYPES -----
//

// EnvironmentSpec defines the desired environment.
// This remains intent-only.
type EnvironmentSpec struct {
	Name       string            `json:"name"`
	Service    string            `json:"service"`
	TTL        time.Duration     `json:"ttl"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// WorkflowReference is a stable identifier for an execution-plane workflow.
// It is immutable and read-only.
/*
type WorkflowReference struct {
	Name        string
	Namespace   string
	Template    string    `json:"template,omitempty"`
	SubmittedAt time.Time `json:"submitted_at,omitempty"`
}*/
type WorkflowReference struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	UID         string `json:"uid"`
	Template    string
	SubmittedAt time.Time
}

// Environment represents the control-plane view of an environment.
// It contains intent + references, but no execution state.
type Environment struct {
	Spec EnvironmentSpec `json:"spec"`

	CreateWorkflow  WorkflowReference  `json:"create_workflow"`
	DestroyWorkflow *WorkflowReference `json:"destroy_workflow,omitempty"`
	TTLWorkflow     *WorkflowReference `json:"ttl_workflow,omitempty"`
}

//
// ----- READ-ONLY VIEWS -----
//

// WorkflowStatusView is derived execution data.
// It is queried on demand and never stored.
/*
type WorkflowStatusView struct {
	Phase      string
	StartedAt  *time.Time
	FinishedAt *time.Time
}
*/ //using wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

//
// ----- ORCHESTRATOR CONTRACT -----
//

// EnvironmentOrchestrator defines control-plane intent submission
// and read-only observability. It does not manage execution.
type EnvironmentOrchestrator interface {
	// Create submits intent to create an environment.
	// Returns an Environment with workflow references populated.
	Create(ctx context.Context, spec EnvironmentSpec) (*Environment, error)

	// Destroy submits intent to destroy an environment.
	//Destroy(ctx context.Context, name string) (*WorkflowReference, error)
	Destroy(ctx context.Context, name string, service string) (*WorkflowReference, error)

	// GetCreateStatus returns the current status of the create workflow.
	//GetCreateStatus(ctx context.Context, env *Environment) (*WorkflowStatusView, error)

	// GetTTLStatus returns the current status of the TTL cleanup workflow.
	//GetTTLStatus(ctx context.Context, env *Environment) (*WorkflowStatusView, error)

	GetCreateStatus(ctx context.Context, env *Environment) (*wf.WorkflowStatus, error)

	GetTTLStatus(ctx context.Context, env *Environment) (*wf.WorkflowStatus, error)

	// Ready verifies connectivity to the authoritative execution plane.
	Ready(ctx context.Context) error
}
