package reconciler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

type SCMCommandReconciler struct {
	store         *api.ServiceStore
	orchestrator  orchestrator.EnvironmentOrchestrator
	previewTTL    time.Duration
	leaseDuration time.Duration
	logger        *zap.Logger
}

func NewSCMCommandReconciler(store *api.ServiceStore, envOrchestrator orchestrator.EnvironmentOrchestrator, previewTTL time.Duration, logger *zap.Logger) *SCMCommandReconciler {
	return &SCMCommandReconciler{store: store, orchestrator: envOrchestrator, previewTTL: previewTTL, leaseDuration: 30 * time.Second, logger: logger}
}

func (r *SCMCommandReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := r.ProcessOne(ctx, time.Now().UTC()); err != nil && !errors.Is(err, api.ErrCommandNotFound) {
			r.logger.Error("SCM command reconciliation failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *SCMCommandReconciler) ProcessOne(ctx context.Context, now time.Time) (bool, error) {
	command, err := r.store.LeaseSCMCommand(now, r.leaseDuration)
	if err != nil {
		return false, err
	}
	processingErr := r.reconcile(ctx, command)
	if completeErr := r.store.CompleteSCMCommand(command.ID, processingErr, now); completeErr != nil {
		return true, fmt.Errorf("complete SCM command: %w", completeErr)
	}
	return true, processingErr
}

func (r *SCMCommandReconciler) reconcile(ctx context.Context, command *scm.LifecycleCommand) error {
	service, err := r.store.FindServiceByRepository(command.Repository)
	if err != nil {
		return fmt.Errorf("resolve registered service for %s: %w", command.Repository, err)
	}
	switch command.Type {
	case scm.EnsurePreviewEnvironment:
		if _, err := r.store.GetEnvironment(command.Environment); err == nil {
			return nil
		} else if !errors.Is(err, api.ErrEnvironmentNotFound) {
			return err
		}
		env, err := r.orchestrator.Create(ctx, orchestrator.EnvironmentSpec{
			Name: command.Environment, Service: service.Name, TTL: r.previewTTL,
			Parameters: map[string]string{"scm_provider": string(command.Provider), "repository": command.Repository, "pull_request": fmt.Sprint(command.PullRequest), "head_sha": command.HeadSHA},
		})
		if err != nil {
			return err
		}
		return r.store.PutEnvironment(env)
	case scm.DestroyPreviewEnvironment:
		env, err := r.store.GetEnvironment(command.Environment)
		if errors.Is(err, api.ErrEnvironmentNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if env.DestroyWorkflow != nil {
			return nil
		}
		ref, err := r.orchestrator.Destroy(ctx, command.Environment, service.Name)
		if err != nil {
			return err
		}
		env.DestroyWorkflow = ref
		return r.store.PutEnvironment(env)
	default:
		return fmt.Errorf("unsupported lifecycle command %q", command.Type)
	}
}
