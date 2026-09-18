package reconciler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/telemetry"
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
	store           *api.ServiceStore
	commands        api.SCMCommandStore
	orchestrator    orchestrator.EnvironmentOrchestrator
	previewTTL      time.Duration
	preview         PreviewRuntimeConfig
	leaseDuration   time.Duration
	statusReporters map[scm.Provider]scm.StatusReporter
	logger          *zap.Logger
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
	GitHubCloneBaseURL      string
}

func NewSCMCommandReconciler(store *api.ServiceStore, commands api.SCMCommandStore, envOrchestrator orchestrator.EnvironmentOrchestrator, previewTTL time.Duration, preview PreviewRuntimeConfig, logger *zap.Logger, configuredReporters ...map[scm.Provider]scm.StatusReporter) *SCMCommandReconciler {
	reporters := map[scm.Provider]scm.StatusReporter{}
	if len(configuredReporters) != 0 && configuredReporters[0] != nil {
		reporters = configuredReporters[0]
	}
	return &SCMCommandReconciler{store: store, commands: commands, orchestrator: envOrchestrator, previewTTL: previewTTL, preview: preview, leaseDuration: 30 * time.Second, statusReporters: reporters, logger: logger}
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
	for _, env := range r.store.ListAllEnvironments() {
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
		tenantStore := r.store.ForTenant(api.TenantID(env.TenantID))
		if _, err := tenantStore.ObserveDeployment(env.Spec.Name, env.DeployWorkflow.Name, env.Spec.Source.Generation, phase, message, observedAt, evidence); err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("persist deployment observation for %s: %w", env.Spec.Name, err))
			continue
		}
		serviceState, serviceMessage := "ready", "observed deployment is healthy"
		if phase == "Failed" || phase == "Error" {
			serviceState, serviceMessage = "degraded", message
		} else if phase != "Succeeded" {
			serviceState, serviceMessage = "reconciling", message
		}
		if err := tenantStore.ObserveService(env.Spec.Service, serviceState, serviceMessage); err != nil && !errors.Is(err, api.ErrServiceNotFound) {
			observationErrors = append(observationErrors, fmt.Errorf("persist service observation for %s: %w", env.Spec.Service, err))
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
	started := time.Now()
	defer func() { telemetry.ObserveReconciliation(time.Since(started)) }()
	tenantID := api.TenantID(command.TenantID)
	if tenantID == "" {
		tenantID = api.DefaultTenantID
		command.TenantID = string(tenantID)
	}
	tenantStore := r.store.ForTenant(tenantID)
	tenantCommands := r.commands
	if scoped, ok := r.commands.(api.TenantScopedCommandStore); ok {
		tenantCommands = scoped.CommandsForTenant(tenantID)
	}
	r.reportStatus(ctx, command, scm.RevisionPending, "Platform preview reconciliation started", "")
	processingErr := r.reconcile(ctx, tenantStore, command)
	if completeErr := tenantCommands.CompleteSCMCommand(command.ID, processingErr, now); completeErr != nil {
		return true, fmt.Errorf("complete SCM command: %w", completeErr)
	}
	state, description, targetURL := scm.RevisionSuccess, "Platform preview is ready", ""
	if processingErr != nil {
		state, description = scm.RevisionFailure, "Platform preview reconciliation failed"
	}
	if environment, lookupErr := tenantStore.GetEnvironment(command.Environment); lookupErr == nil && environment.Spec.Source != nil {
		targetURL = environment.Spec.Source.PreviewURL
	}
	r.reportStatus(ctx, command, state, description, targetURL)
	return true, processingErr
}

func (r *SCMCommandReconciler) reportStatus(ctx context.Context, command *scm.LifecycleCommand, state scm.RevisionState, description, targetURL string) {
	reporter := r.statusReporters[command.Provider]
	if reporter == nil || command.HeadSHA == "" {
		return
	}
	if err := reporter.Report(ctx, scm.RevisionStatus{Repository: command.Repository, InstallationID: command.InstallationID, SHA: command.HeadSHA, State: state, Description: description, TargetURL: targetURL}); err != nil {
		// Status publication is observational: provider impairment must not mutate
		// an already-durable lifecycle outcome or induce duplicate deployments.
		r.logger.Warn("SCM revision status publication failed", zap.String("provider", string(command.Provider)), zap.Error(err))
	}
}

func (r *SCMCommandReconciler) reconcile(ctx context.Context, store *api.ServiceStore, command *scm.LifecycleCommand) error {
	service, err := store.FindServiceByRepository(command.Repository)
	if err != nil {
		return fmt.Errorf("resolve registered service for %s: %w", command.Repository, err)
	}
	switch command.Type {
	case scm.EnsurePreviewEnvironment:
		env, err := store.GetEnvironment(command.Environment)
		if err != nil && !errors.Is(err, api.ErrEnvironmentNotFound) {
			return err
		}
		if errors.Is(err, api.ErrEnvironmentNotFound) {
			env, err = r.orchestrator.Create(ctx, orchestrator.EnvironmentSpec{TenantID: command.TenantID, Name: command.Environment, Namespace: orchestrator.NamespaceForTenant(command.TenantID, command.Environment), Service: service.Name, TTL: r.previewTTL})
			if err != nil {
				return err
			}
			// Persist provisioning identity before deployment. If deployment
			// submission fails, retrying must not submit another create workflow.
			env.TenantID = command.TenantID
			if err := store.PutEnvironment(env); err != nil {
				return err
			}
		}
		// The deployment workflow runs as a ServiceAccount provisioned by the
		// create workflow. Refuse to race Pod admission against that identity and
		// its namespace-local RoleBinding; durable command retry will resume once
		// namespace provisioning is authoritative.
		createStatus, err := r.orchestrator.GetCreateStatus(ctx, env)
		if err != nil {
			return fmt.Errorf("observe namespace provisioning: %w", err)
		}
		if createStatus == nil || createStatus.Phase == wf.WorkflowPending || createStatus.Phase == wf.WorkflowRunning || createStatus.Phase == "" {
			return fmt.Errorf("namespace provisioning workflow has not completed")
		}
		if createStatus.Phase != wf.WorkflowSucceeded {
			return fmt.Errorf("namespace provisioning workflow %s: %s", createStatus.Phase, createStatus.Message)
		}
		deployment, err := r.previewDeployment(service, env.Spec.Namespace, command.HeadSHA)
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
		cloneURL := service.RepoURL
		if command.Provider == scm.ProviderGitHub && strings.TrimSpace(r.preview.GitHubCloneBaseURL) != "" {
			cloneURL = strings.TrimRight(r.preview.GitHubCloneBaseURL, "/") + "/" + strings.TrimPrefix(command.Repository, "/") + ".git"
		}
		env.Spec.Source = &orchestrator.SourceRevision{
			Provider: string(command.Provider), Repository: command.Repository, CloneURL: cloneURL,
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
		return store.PutEnvironment(env)
	case scm.DestroyPreviewEnvironment:
		env, err := store.GetEnvironment(command.Environment)
		if errors.Is(err, api.ErrEnvironmentNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if env.DestroyWorkflow != nil {
			return nil
		}
		ref, err := r.orchestrator.Destroy(ctx, env.Spec.Namespace, service.Name, command.TenantID)
		if err != nil {
			return err
		}
		env.DestroyWorkflow = ref
		return store.PutEnvironment(env)
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
	tenantID := string(service.TenantID)
	if tenantID == "" {
		tenantID = string(api.DefaultTenantID)
	}
	if signingProfile != "key" && signingProfile != "kms" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_SIGNING_PROFILE must be key or kms")
	}
	if signingProfile == "key" && strings.TrimSpace(r.preview.CosignPrivateKeySecret) == "" {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_PRIVATE_KEY_SECRET is required for the key signing profile")
	}
	if signingProfile == "kms" && !kmsSigner.MatchString(strings.TrimSpace(r.preview.CosignSigner)) {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_SIGNER must be a supported KMS URI for the kms signing profile")
	}
	if signingProfile == "kms" && !strings.Contains(r.preview.CosignSigner, "{tenant}") {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("PREVIEW_COSIGN_SIGNER must contain {tenant} for tenant-partitioned KMS signing")
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
	tenantRepository := repository + "/" + tenantID
	imageRef := tenantRepository + "/" + imageName + ":" + strings.ToLower(sha)
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
	egressPolicy, err := encodeEgressPolicy(service, tenantID)
	if err != nil {
		return orchestrator.PreviewDeployment{}, fmt.Errorf("encode service egress policy: %w", err)
	}
	return orchestrator.PreviewDeployment{
		ProjectType: service.ProjectType, ImageRef: imageRef, ImageRepository: tenantRepository + "/" + imageName, ContainerPort: port,
		Dockerfile: dockerfile, PreviewHost: host, PreviewURL: previewURL,
		BuilderImage: r.preview.BuilderImage, RegistrySecretName: r.preview.RegistrySecretName,
		RegistryInsecure: r.preview.RegistryInsecure,
		ScannerImage:     r.preview.ScannerImage, VulnerabilitySeverities: severities,
		IgnoreUnfixed:  r.preview.IgnoreUnfixed,
		TargetPlatform: platform,
		CosignImage:    r.preview.CosignImage, CosignSigner: tenantSigner(r.preview.CosignSigner, tenantID, signingProfile), SigningProfile: signingProfile, CosignPrivateKeySecret: tenantArtifactName(r.preview.CosignPrivateKeySecret, tenantID),
		CosignAuthMode: authMode, VaultImage: r.preview.VaultImage, VaultAddress: r.preview.VaultAddress, VaultRole: r.preview.VaultRole,
		CosignPublicKeySecret: tenantArtifactName(r.preview.CosignPublicKeySecret, tenantID), PolicyPredicateType: r.preview.PolicyPredicateType, VEXConfigMap: tenantArtifactName(r.preview.VEXConfigMap, tenantID),
		EgressPolicy: egressPolicy,
	}, nil
}

func encodeEgressPolicy(service api.Service, tenantID string) (string, error) {
	dnsRules := make([]map[string]string, 0, len(service.Deployment.Egress))
	egress := make([]any, 0, len(service.Deployment.Egress)+1)
	for _, rule := range service.Deployment.Egress {
		dnsRules = append(dnsRules, map[string]string{"matchName": rule.DNSName})
		egress = append(egress, map[string]any{
			"toFQDNs": []map[string]string{{"matchName": rule.DNSName}},
			"toPorts": []any{map[string]any{"ports": []map[string]string{{"port": fmt.Sprintf("%d", rule.Port), "protocol": rule.Protocol}}}},
		})
	}
	if len(dnsRules) != 0 {
		egress = append([]any{map[string]any{
			"toEndpoints": []any{map[string]any{"matchLabels": map[string]string{"k8s:io.kubernetes.pod.namespace": "kube-system", "k8s:k8s-app": "kube-dns"}}},
			"toPorts": []any{map[string]any{
				"ports": []map[string]string{{"port": "53", "protocol": "UDP"}, {"port": "53", "protocol": "TCP"}},
				"rules": map[string]any{"dns": dnsRules},
			}},
		}}, egress...)
	}
	manifest := map[string]any{
		"apiVersion": "cilium.io/v2", "kind": "CiliumNetworkPolicy",
		"metadata": map[string]any{"name": "service-egress", "labels": map[string]string{"platform.tenant": tenantID, "platform.service": service.Name}},
		"spec":     map[string]any{"endpointSelector": map[string]any{"matchLabels": map[string]string{"app.kubernetes.io/name": "preview"}}, "egress": egress},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encoded), nil
}

func tenantSigner(signer, tenantID, profile string) string {
	if profile == "kms" {
		return strings.ReplaceAll(signer, "{tenant}", tenantID)
	}
	return signer
}

func tenantArtifactName(base, tenantID string) string {
	if base == "" {
		return base
	}
	value := strings.Trim(strings.ToLower(base+"-"+tenantID), "-")
	if len(value) <= 63 {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return strings.TrimRight(value[:54], "-") + "-" + fmt.Sprintf("%x", digest[:4])
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
