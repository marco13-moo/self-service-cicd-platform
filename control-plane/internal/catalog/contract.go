// Package catalog contains the provider-neutral service declaration contract
// and the small state machine shared by API and reconciliation code.
package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/policy"
)

const (
	APIVersion = "platform.service/v1"
	Kind       = "Service"
)

type Declaration struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

type Metadata struct {
	Name string `json:"name" yaml:"name"`
}

type Spec struct {
	Owner        string             `json:"owner" yaml:"owner"`
	Repository   string             `json:"repository" yaml:"repository"`
	Dependencies []string           `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	SLO          SLOIntent          `json:"slo,omitempty" yaml:"slo,omitempty"`
	Compliance   ComplianceMetadata `json:"compliance,omitempty" yaml:"compliance,omitempty"`
	Runtime      RuntimeIntent      `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Policy       policy.Declaration `json:"policy,omitempty" yaml:"policy,omitempty"`
	Lifecycle    string             `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
}

type SLOIntent struct {
	Availability string `json:"availability,omitempty" yaml:"availability,omitempty"`
	Latency      string `json:"latency,omitempty" yaml:"latency,omitempty"`
}

type ComplianceMetadata struct {
	DataClass   string   `json:"dataClass,omitempty" yaml:"dataClass,omitempty"`
	Controls    []string `json:"controls,omitempty" yaml:"controls,omitempty"`
	Attestation string   `json:"attestation,omitempty" yaml:"attestation,omitempty"`
}

type RuntimeIntent struct {
	Environment   string `json:"environment,omitempty" yaml:"environment,omitempty"`
	ContainerPort int    `json:"containerPort,omitempty" yaml:"containerPort,omitempty"`
	Dockerfile    string `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
}

type Status struct {
	DesiredGeneration  int64  `json:"desiredGeneration"`
	ObservedGeneration int64  `json:"observedGeneration"`
	DesiredState       string `json:"desiredState"`
	ObservedState      string `json:"observedState"`
	Message            string `json:"message,omitempty"`
}

func (d Declaration) Validate() error {
	if d.APIVersion != APIVersion || d.Kind != Kind {
		return fmt.Errorf("unsupported service contract: apiVersion=%q kind=%q", d.APIVersion, d.Kind)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return errors.New("metadata.name is required")
	}
	if strings.TrimSpace(d.Spec.Owner) == "" {
		return errors.New("spec.owner is required")
	}
	if strings.TrimSpace(d.Spec.Repository) == "" {
		return errors.New("spec.repository is required")
	}
	if d.Spec.Lifecycle == "" {
		d.Spec.Lifecycle = "active"
	}
	switch d.Spec.Lifecycle {
	case "active", "paused", "retired":
	default:
		return fmt.Errorf("spec.lifecycle must be active, paused, or retired")
	}
	return nil
}
