package orchestrator

import (
	"context"
	"testing"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type submission struct {
	template   string
	parameters map[string]string
}

type recordingExecutor struct {
	submissions []submission
	workflows   map[string]*wf.Workflow
}

func (e *recordingExecutor) SubmitFromTemplate(_ context.Context, template, generateName string, parameters, _ map[string]string) (*wf.Workflow, error) {
	copyOfParameters := make(map[string]string, len(parameters))
	for key, value := range parameters {
		copyOfParameters[key] = value
	}
	e.submissions = append(e.submissions, submission{template: template, parameters: copyOfParameters})
	return &wf.Workflow{ObjectMeta: metav1.ObjectMeta{Name: generateName + "test", Namespace: "argo"}}, nil
}

func (e *recordingExecutor) GetWorkflow(_ context.Context, name string) (*wf.Workflow, error) {
	return e.workflows[name], nil
}
func (*recordingExecutor) Cancel(context.Context, string) error { return nil }
func (*recordingExecutor) Ready(context.Context) error          { return nil }

func TestCreatePersistsExpiryAndSubmitsDurableTTLSuspension(t *testing.T) {
	executor := &recordingExecutor{}
	orchestrator := NewArgoEnvironmentOrchestrator(executor)
	expiresAt := time.Date(2026, time.September, 2, 18, 30, 0, 0, time.UTC)

	env, err := orchestrator.Create(context.Background(), EnvironmentSpec{
		Name: "checkout-pr-42", Service: "checkout", TTL: 90 * time.Minute, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !env.Spec.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expiry was not retained: got %s want %s", env.Spec.ExpiresAt, expiresAt)
	}
	if len(executor.submissions) != 2 || executor.submissions[1].template != "env-ttl-cleanup-template" {
		t.Fatalf("unexpected submissions: %#v", executor.submissions)
	}
	ttl := executor.submissions[1].parameters
	if ttl["ttl_duration"] != "1h30m0s" || ttl["expires_at"] != expiresAt.Format(time.RFC3339) {
		t.Fatalf("unexpected TTL parameters: %#v", ttl)
	}
}

func TestGetDeployStatusReadsCurrentWorkflow(t *testing.T) {
	executor := &recordingExecutor{workflows: map[string]*wf.Workflow{
		"deploy-2": {Status: wf.WorkflowStatus{Phase: wf.WorkflowSucceeded}},
	}}
	orchestrator := NewArgoEnvironmentOrchestrator(executor)
	status, err := orchestrator.GetDeployStatus(context.Background(), &Environment{
		DeployWorkflow: &WorkflowReference{Name: "deploy-2", Namespace: "argo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.Phase != wf.WorkflowSucceeded {
		t.Fatalf("unexpected deployment status: %#v", status)
	}
}

func TestDeploySubmitsImmutableImageAndRoutingContract(t *testing.T) {
	executor := &recordingExecutor{}
	orchestrator := NewArgoEnvironmentOrchestrator(executor)
	env := &Environment{Spec: EnvironmentSpec{Name: "checkout-pr-42", Service: "checkout", Source: &SourceRevision{
		CloneURL: "https://example.test/acme/checkout.git", DesiredSHA: "abc123",
	}}}
	_, err := orchestrator.Deploy(context.Background(), env, PreviewDeployment{
		ProjectType: "go", ImageRef: "registry.test/previews/checkout:abc123", ImageRepository: "registry.test/previews/checkout", ContainerPort: 8080,
		Dockerfile: "deploy/Dockerfile", PreviewHost: "checkout-pr-42.preview.test",
		PreviewURL: "https://checkout-pr-42.preview.test", BuilderImage: "buildkit:test",
		RegistrySecretName: "registry-credentials", RegistryInsecure: true,
		ScannerImage: "trivy:test", VulnerabilitySeverities: "HIGH,CRITICAL", IgnoreUnfixed: true,
		CosignImage: "cosign:test", CosignSigner: "gcpkms://projects/test/key", SigningProfile: "kms", CosignPrivateKeySecret: "cosign-private", CosignPublicKeySecret: "cosign-public", PolicyPredicateType: "https://example.test/policy/v1", VEXConfigMap: "preview-vex-none",
		TargetPlatform: "linux/arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters := executor.submissions[0].parameters
	if parameters["image_ref"] != "registry.test/previews/checkout:abc123" || parameters["container_port"] != "8080" || parameters["registry_insecure"] != "true" || parameters["scanner_image"] != "trivy:test" || parameters["vulnerability_severities"] != "HIGH,CRITICAL" || parameters["ignore_unfixed"] != "true" || parameters["target_platform"] != "linux/arm64" {
		t.Fatalf("unexpected deployment parameters: %#v", parameters)
	}
	if parameters["cosign_image"] != "cosign:test" || parameters["cosign_private_key_secret"] != "cosign-private" || parameters["cosign_public_key_secret"] != "cosign-public" || parameters["vex_config_map"] != "preview-vex-none" {
		t.Fatalf("unexpected trust parameters: %#v", parameters)
	}
	if parameters["cosign_signer"] != "gcpkms://projects/test/key" || parameters["signing_profile"] != "kms" || parameters["policy_predicate_type"] != "https://example.test/policy/v1" {
		t.Fatalf("unexpected signer parameters: %#v", parameters)
	}
	if parameters["image_repository"] != "registry.test/previews/checkout" {
		t.Fatalf("unexpected immutable image repository: %#v", parameters)
	}
}
