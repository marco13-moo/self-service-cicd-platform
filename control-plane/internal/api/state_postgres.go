package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

func (s *ServiceStore) putServicePostgres(service Service) error {
	if service.Version == 0 {
		service.Version = 1
	}
	document, err := json.Marshal(service)
	if err != nil {
		return fmt.Errorf("encode service: %w", err)
	}
	result, err := s.db.Exec(`INSERT INTO services(name,document,version) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, service.Name, document, service.Version)
	if err != nil {
		return fmt.Errorf("persist service: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (s *ServiceStore) getServicePostgres(name string) (Service, error) {
	var document []byte
	if err := s.db.QueryRow(`SELECT document FROM services WHERE name=$1`, name).Scan(&document); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Service{}, ErrServiceNotFound
		}
		return Service{}, err
	}
	var service Service
	if err := json.Unmarshal(document, &service); err != nil {
		return Service{}, fmt.Errorf("decode service: %w", err)
	}
	return service, nil
}

func (s *ServiceStore) listServicesPostgres() []Service {
	rows, err := s.db.Query(`SELECT document FROM services ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var services []Service
	for rows.Next() {
		var document []byte
		var service Service
		if rows.Scan(&document) == nil && json.Unmarshal(document, &service) == nil {
			services = append(services, service)
		}
	}
	return services
}

func (s *ServiceStore) putEnvironmentPostgres(env *orchestrator.Environment) error {
	if env == nil {
		return errors.New("environment is required")
	}
	next := cloneEnvironment(env)
	next.Version++
	document, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode environment: %w", err)
	}
	var result sql.Result
	if env.Version == 0 {
		result, err = s.db.Exec(`INSERT INTO environments(name,document,version) VALUES($1,$2,1) ON CONFLICT DO NOTHING`, env.Spec.Name, document)
	} else {
		result, err = s.db.Exec(`UPDATE environments SET document=$1,version=$2,updated_at=now() WHERE name=$3 AND version=$4`, document, next.Version, env.Spec.Name, env.Version)
	}
	if err != nil {
		return fmt.Errorf("persist environment: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrVersionConflict
	}
	env.Version = next.Version
	return nil
}

func (s *ServiceStore) getEnvironmentPostgres(name string) (*orchestrator.Environment, error) {
	var document []byte
	if err := s.db.QueryRow(`SELECT document FROM environments WHERE name=$1`, name).Scan(&document); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEnvironmentNotFound
		}
		return nil, err
	}
	var env orchestrator.Environment
	if err := json.Unmarshal(document, &env); err != nil {
		return nil, fmt.Errorf("decode environment: %w", err)
	}
	return &env, nil
}

func (s *ServiceStore) listEnvironmentsPostgres() []*orchestrator.Environment {
	rows, err := s.db.Query(`SELECT document FROM environments ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var environments []*orchestrator.Environment
	for rows.Next() {
		var document []byte
		var env orchestrator.Environment
		if rows.Scan(&document) == nil && json.Unmarshal(document, &env) == nil {
			environments = append(environments, &env)
		}
	}
	return environments
}

func (s *ServiceStore) observeDeploymentPostgres(name, workflowName string, generation int64, phase, message string, observedAt time.Time, evidence DeploymentEvidence) (bool, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var document []byte
	var version int64
	if err = tx.QueryRow(`SELECT document,version FROM environments WHERE name=$1 FOR UPDATE`, name).Scan(&document, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrEnvironmentNotFound
		}
		return false, err
	}
	var env orchestrator.Environment
	if err = json.Unmarshal(document, &env); err != nil {
		return false, fmt.Errorf("decode environment: %w", err)
	}
	if env.Spec.Source == nil || env.DeployWorkflow == nil || env.DeployWorkflow.Name != workflowName || env.Spec.Source.Generation != generation {
		return false, nil
	}
	source := env.Spec.Source
	if source.DeploymentPhase == phase && source.DeploymentMessage == message && !(phase == "Succeeded" && (source.DeployedSHA != source.DesiredSHA || source.DeployedImage != evidence.DeployedImage || source.PreviewURL != source.DesiredPreviewURL)) {
		return false, nil
	}
	stamp := observedAt.UTC()
	source.DeploymentPhase, source.DeploymentMessage, source.ObservedAt = phase, message, &stamp
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
	env.Version = version + 1
	updatedDocument, err := json.Marshal(env)
	if err != nil {
		return false, fmt.Errorf("encode environment: %w", err)
	}
	result, err := tx.Exec(`UPDATE environments SET document=$1,version=$2,updated_at=now() WHERE name=$3 AND version=$4`, updatedDocument, env.Version, name, version)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return false, ErrVersionConflict
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
