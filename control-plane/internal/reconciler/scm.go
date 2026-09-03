package reconciler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

var (
	imageComponentSanitizer = regexp.MustCompile(`[^a-z0-9._-]+`)
	commitSHA               = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)
	imageDigest             = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	vulnerabilitySeverities = regexp.MustCompile(`^(UNKNOWN|LOW|MEDIUM|HIGH|CRITICAL)(,(UNKNOWN|LOW|MEDIUM|HIGH|CRITICAL))*$`)
	kmsSigner               = regexp.MustCompile(`^(awskms|gcpkms|azurekms|hashivault)://`)
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
	ImageRepository         string
	BaseDomain              string
	URLScheme               string
	BuilderImage            string
	RegistrySecretName      string
	RegistryInsecure        bool
	ScannerImage            string
	VulnerabilitySeverities string
	IgnoreUnfixed           bool
	TargetPlatform          string
	CosignImage             string
	CosignSigner            string
	SigningProfile          string
	CosignAuthMode          string
	VaultImage              string
	VaultAddress            string
	VaultRole               string
	CosignPrivateKeySecret  string
	CosignPublicKeySecret   string
	PolicyPredicateType     string
	VEXConfigMap            string
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
		phase, message := string(status.Phase), status.Message
		evidence := api.DeploymentEvidence{}
		if status.Phase == wf.WorkflowSucceeded {
			digest := workflowOutput(status, "image-digest")
			if !imageDigest.MatchString(digest) {
				phase, message = "Error", "deployment workflow succeeded without a valid image digest"
			} else {
				immutableImage := digestReference(env.Spec.Source.DesiredImage, digest)
				evidence = api.DeploymentEvidence{ImageDigest: digest, DeployedImage: immutableImage, SBOMReference: immutableImage, ProvenanceReference: immutableImage, VulnerabilityPolicy: "passed", SignatureReference: immutableImage, PolicyAttestation: immutableImage}
			}
		}
		if _, err := r.store.ObserveDeployment(env.Spec.Name, env.DeployWorkflow.Name, env.Spec.Source.Generation, phase, message, observedAt, evidence); err != nil {
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
		imageDigestValue, sbomReference, provenanceReference, vulnerabilityPolicy, signatureReference, policyAttestation := "", "", "", "", "", ""
		if env.Spec.Source != nil {
			deployedImage = env.Spec.Source.DeployedImage
			previewURL = env.Spec.Source.PreviewURL
			imageDigestValue = env.Spec.Source.ImageDigest
			sbomReference = env.Spec.Source.SBOMReference
			provenanceReference = env.Spec.Source.ProvenanceReference
			vulnerabilityPolicy = env.Spec.Source.VulnerabilityPolicy
			signatureReference = env.Spec.Source.SignatureReference
			policyAttestation = env.Spec.Source.PolicyAttestation
		}
		env.Spec.Source = &orchestrator.SourceRevision{
			Provider: string(command.Provider), Repository: command.Repository, CloneURL: service.RepoURL,
			PullRequest: command.PullRequest, DesiredSHA: command.HeadSHA, DeployedSHA: deployed,
			Generation: generation, DeploymentPhase: "Pending", DesiredImage: deployment.ImageRef,
			DeployedImage: deployedImage, DesiredPreviewURL: deployment.PreviewURL, PreviewURL: previewURL,
			ImageDigest: imageDigestValue, SBOMReference: sbomReference, ProvenanceReference: provenanceReference,
			VulnerabilityPolicy: vulnerabilityPolicy,
			SignatureReference:  signatureReference, PolicyAttestation: policyAttestation,
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
	if strings.TrimSpace(r.preview.ScannerImage) == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_SCANNER_IMAGE is required for preview policy evaluation")
	}
	if strings.TrimSpace(r.preview.CosignImage) == "" || strings.TrimSpace(r.preview.CosignSigner) == "" || strings.TrimSpace(r.preview.CosignPublicKeySecret) == "" || strings.TrimSpace(r.preview.PolicyPredicateType) == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("Cosign image, signer, public-key Secret, and policy predicate type are required")
	}
	signingProfile := strings.ToLower(strings.TrimSpace(r.preview.SigningProfile))
	if signingProfile != "key" && signingProfile != "kms" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_SIGNING_PROFILE must be key or kms")
	}
	if signingProfile == "key" && strings.TrimSpace(r.preview.CosignPrivateKeySecret) == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_PRIVATE_KEY_SECRET is required for the key signing profile")
	}
	if signingProfile == "kms" && !kmsSigner.MatchString(strings.TrimSpace(r.preview.CosignSigner)) {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_SIGNER must be a supported KMS URI for the kms signing profile")
	}
	authMode := strings.ToLower(strings.TrimSpace(r.preview.CosignAuthMode))
	if authMode != "ambient" && authMode != "vault-kubernetes" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_AUTH_MODE must be ambient or vault-kubernetes")
	}
	if authMode == "vault-kubernetes" && (!strings.HasPrefix(r.preview.CosignSigner, "hashivault://") || strings.TrimSpace(r.preview.VaultImage) == "" || strings.TrimSpace(r.preview.VaultAddress) == "" || strings.TrimSpace(r.preview.VaultRole) == "") {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("vault-kubernetes auth requires a hashivault signer, Vault image, address, and role")
	}
	severities := strings.ToUpper(strings.TrimSpace(r.preview.VulnerabilitySeverities))
	if !vulnerabilitySeverities.MatchString(severities) {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_VULNERABILITY_SEVERITIES contains an unsupported severity set")
	}
	platform := strings.TrimSpace(r.preview.TargetPlatform)
	if platform != "linux/amd64" && platform != "linux/arm64" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_TARGET_PLATFORM must be linux/amd64 or linux/arm64")
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
		ProjectType: service.ProjectType, ImageRef: imageRef, ImageRepository: repository + "/" + imageName, ContainerPort: port,
		Dockerfile: dockerfile, PreviewHost: host, PreviewURL: previewURL,
		BuilderImage: r.preview.BuilderImage, RegistrySecretName: r.preview.RegistrySecretName,
		RegistryInsecure: r.preview.RegistryInsecure,
		ScannerImage:     r.preview.ScannerImage, VulnerabilitySeverities: severities,
		IgnoreUnfixed:  r.preview.IgnoreUnfixed,
		TargetPlatform: platform,
		CosignImage:    r.preview.CosignImage, CosignSigner: r.preview.CosignSigner, SigningProfile: signingProfile, CosignPrivateKeySecret: r.preview.CosignPrivateKeySecret,
		CosignAuthMode: authMode, VaultImage: r.preview.VaultImage, VaultAddress: r.preview.VaultAddress, VaultRole: r.preview.VaultRole,
		CosignPublicKeySecret: r.preview.CosignPublicKeySecret, PolicyPredicateType: r.preview.PolicyPredicateType, VEXConfigMap: r.preview.VEXConfigMap,
	}, nil
}

func workflowOutput(status *wf.WorkflowStatus, name string) string {
	if status == nil || status.Outputs == nil {
		return ""
	}
	for _, parameter := range status.Outputs.Parameters {
		if parameter.Name == name && parameter.Value != nil {
			return parameter.Value.String()
		}
	}
	return ""
}

func digestReference(tag, digest string) string {
	lastSlash, lastColon := strings.LastIndex(tag, "/"), strings.LastIndex(tag, ":")
	if lastColon > lastSlash {
		tag = tag[:lastColon]
	}
	return tag + "@" + digest
}
