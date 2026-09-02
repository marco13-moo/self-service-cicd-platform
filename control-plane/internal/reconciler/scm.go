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
	commands      api.SCMCommandStore
	orchestrator  orchestrator.EnvironmentOrchestrator
	previewTTL    time.Duration
	leaseDuration time.Duration
	logger        *zap.Logger
}

func NewSCMCommandReconciler(store *api.ServiceStore, commands api.SCMCommandStore, envOrchestrator orchestrator.EnvironmentOrchestrator, previewTTL time.Duration, logger *zap.Logger) *SCMCommandReconciler {
	return &SCMCommandReconciler{store: store, commands: commands, orchestrator: envOrchestrator, previewTTL: previewTTL, leaseDuration: 30 * time.Second, logger: logger}
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
	command, err := r.commands.LeaseSCMCommand(now, r.leaseDuration)
	if err != nil {
		return false, err
	}
	processingErr := r.reconcile(ctx, command)
	if completeErr := r.commands.CompleteSCMCommand(command.ID, processingErr, now); completeErr != nil {
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
		env, err := r.store.GetEnvironment(command.Environment)
		if err != nil && !errors.Is(err, api.ErrEnvironmentNotFound) {
			return err
		}
		if errors.Is(err, api.ErrEnvironmentNotFound) {
			env, err = r.orchestrator.Create(ctx, orchestrator.EnvironmentSpec{Name: command.Environment, Service: service.Name, TTL: r.previewTTL})
			if err != nil {
				return err
			}
			// Persist provisioning identity before deployment. If deployment
			// submission fails, retrying must not submit another create workflow.
			if err := r.store.PutEnvironment(env); err != nil {
				return err
			}
		}
		if env.Spec.Source != nil && env.Spec.Source.DesiredSHA == command.HeadSHA && env.DeployWorkflow != nil {
			return nil
		}
		generation := int64(1)
		deployed := ""
		if env.Spec.Source != nil {
			generation = env.Spec.Source.Generation + 1
			deployed = env.Spec.Source.DeployedSHA
		}
		env.Spec.Source = &orchestrator.SourceRevision{Provider: string(command.Provider), Repository: command.Repository, CloneURL: service.RepoURL, PullRequest: command.PullRequest, DesiredSHA: command.HeadSHA, DeployedSHA: deployed, Generation: generation}
		ref, err := r.orchestrator.Deploy(ctx, env, service.ProjectType)
		if err != nil {
			return err
		}
		env.DeployWorkflow = ref
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
