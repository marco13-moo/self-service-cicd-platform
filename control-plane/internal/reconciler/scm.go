package reconciler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

var (
	imageComponentSanitizer = regexp.MustCompile(`[^a-z0-9._-]+`)
	commitSHA               = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)
)

type SCMCommandReconciler struct {
	store         *api.ServiceStore
	commands      api.SCMCommandStore
	orchestrator  orchestrator.EnvironmentOrchestrator
	previewTTL    time.Duration
	preview       PreviewRuntimeConfig
	leaseDuration time.Duration
	logger        *zap.Logger
}

type PreviewRuntimeConfig struct {
	ImageRepository    string
	BaseDomain         string
	URLScheme          string
	BuilderImage       string
	RegistrySecretName string
	RegistryInsecure   bool
}

func NewSCMCommandReconciler(store *api.ServiceStore, commands api.SCMCommandStore, envOrchestrator orchestrator.EnvironmentOrchestrator, previewTTL time.Duration, preview PreviewRuntimeConfig, logger *zap.Logger) *SCMCommandReconciler {
	return &SCMCommandReconciler{store: store, commands: commands, orchestrator: envOrchestrator, previewTTL: previewTTL, preview: preview, leaseDuration: 30 * time.Second, logger: logger}
}

func (r *SCMCommandReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		now := time.Now().UTC()
		if err := r.ObserveDeployments(ctx, now); err != nil {
			r.logger.Error("deployment observation failed", zap.Error(err))
		}
		if _, err := r.ProcessOne(ctx, now); err != nil && !errors.Is(err, api.ErrCommandNotFound) {
			r.logger.Error("SCM command reconciliation failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ObserveDeployments projects live Argo outcomes into durable control-plane
// state. The store performs the generation/workflow compare-and-set, making a
// late observation from an obsolete deployment harmless.
func (r *SCMCommandReconciler) ObserveDeployments(ctx context.Context, observedAt time.Time) error {
	var observationErrors []error
	for _, env := range r.store.ListEnvironments() {
		if env.Spec.Source == nil || env.DeployWorkflow == nil || deploymentTerminal(env.Spec.Source.DeploymentPhase) {
			continue
		}
		status, err := r.orchestrator.GetDeployStatus(ctx, env)
		if err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("observe deployment %s: %w", env.DeployWorkflow.Name, err))
			continue
		}
		if status == nil || status.Phase == "" {
			continue
		}
		if _, err := r.store.ObserveDeployment(env.Spec.Name, env.DeployWorkflow.Name, env.Spec.Source.Generation, string(status.Phase), status.Message, observedAt); err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("persist deployment observation for %s: %w", env.Spec.Name, err))
		}
	}
	return errors.Join(observationErrors...)
}

func deploymentTerminal(phase string) bool {
	switch phase {
	case "Succeeded", "Failed", "Error":
		return true
	default:
		return false
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
		deployment, err := r.previewDeployment(service, env.Spec.Name, command.HeadSHA)
		if err != nil {
			return err
		}
		if env.Spec.Source != nil && env.Spec.Source.DesiredSHA == command.HeadSHA && env.Spec.Source.DesiredImage == deployment.ImageRef && env.DeployWorkflow != nil {
			return nil
		}
		generation := int64(1)
		deployed := ""
		if env.Spec.Source != nil {
			generation = env.Spec.Source.Generation + 1
			deployed = env.Spec.Source.DeployedSHA
		}
		deployedImage := ""
		previewURL := ""
		if env.Spec.Source != nil {
			deployedImage = env.Spec.Source.DeployedImage
			previewURL = env.Spec.Source.PreviewURL
		}
		env.Spec.Source = &orchestrator.SourceRevision{
			Provider: string(command.Provider), Repository: command.Repository, CloneURL: service.RepoURL,
			PullRequest: command.PullRequest, DesiredSHA: command.HeadSHA, DeployedSHA: deployed,
			Generation: generation, DeploymentPhase: "Pending", DesiredImage: deployment.ImageRef,
			DeployedImage: deployedImage, DesiredPreviewURL: deployment.PreviewURL, PreviewURL: previewURL,
		}
		ref, err := r.orchestrator.Deploy(ctx, env, deployment)
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

func (r *SCMCommandReconciler) previewDeployment(service api.Service, environment, sha string) (orchestrator.PreviewDeployment, error) {
	repository := strings.TrimSuffix(strings.TrimSpace(r.preview.ImageRepository), "/")
	if repository == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_IMAGE_REPOSITORY is required for preview builds")
	}
	if strings.TrimSpace(r.preview.BuilderImage) == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_BUILDER_IMAGE is required for preview builds")
	}
	port := service.Deployment.ContainerPort
	if port == 0 {
		port = 8080
	}
	dockerfile := service.Deployment.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	imageName := service.Repository.Name
	if imageName == "" {
		imageName = service.Name
	}
	imageName = strings.Trim(imageComponentSanitizer.ReplaceAllString(strings.ToLower(imageName), "-"), ".-_")
	if imageName == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("service %q does not yield a valid OCI repository component", service.Name)
	}
	if !commitSHA.MatchString(sha) {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("source revision must be a hexadecimal commit SHA")
	}
	imageRef := repository + "/" + imageName + ":" + strings.ToLower(sha)
	baseDomain := strings.Trim(strings.TrimSpace(r.preview.BaseDomain), ".")
	host := ""
	previewURL := fmt.Sprintf("http://preview.%s.svc.cluster.local:%d", environment, port)
	if baseDomain != "" {
		host = environment + "." + baseDomain
		scheme := strings.TrimSpace(r.preview.URLScheme)
		if scheme == "" {
			scheme = "https"
		}
		previewURL = scheme + "://" + host
	}
	return orchestrator.PreviewDeployment{
		ProjectType: service.ProjectType, ImageRef: imageRef, ContainerPort: port,
		Dockerfile: dockerfile, PreviewHost: host, PreviewURL: previewURL,
		BuilderImage: r.preview.BuilderImage, RegistrySecretName: r.preview.RegistrySecretName,
		RegistryInsecure: r.preview.RegistryInsecure,
	}, nil
}
