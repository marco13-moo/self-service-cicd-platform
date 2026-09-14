package reconciler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

func TestPostgresReconciliationContinuesAfterReplicaTermination(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	primaryDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	secondaryDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer secondaryDB.Close()
	primaryState, err := api.NewPostgresServiceStore(context.Background(), primaryDB)
	if err != nil {
		t.Fatal(err)
	}
	secondaryState, err := api.NewPostgresServiceStore(context.Background(), secondaryDB)
	if err != nil {
		t.Fatal(err)
	}
	primaryCommands, err := api.NewPostgresCommandStore(context.Background(), primaryDB)
	if err != nil {
		t.Fatal(err)
	}
	secondaryCommands, err := api.NewPostgresCommandStore(context.Background(), secondaryDB)
	if err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceName := "reconcile-ha-" + suffix
	environmentName := serviceName + "-pr-7"
	t.Cleanup(func() {
		_, _ = secondaryDB.Exec(`DELETE FROM environments WHERE name=$1`, environmentName)
		_, _ = secondaryDB.Exec(`DELETE FROM services WHERE name=$1`, serviceName)
		_, _ = secondaryDB.Exec(`DELETE FROM scm_commands WHERE environment=$1`, environmentName)
		_, _ = secondaryDB.Exec(`DELETE FROM scm_deliveries WHERE delivery_id=$1`, suffix)
	})
	if err = primaryState.Put(api.Service{Name: serviceName, RepoURL: "https://github.com/acme/" + serviceName, Version: 1}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	command := &scm.LifecycleCommand{ID: "github:" + suffix, Provider: scm.ProviderGitHub, DeliveryID: suffix, Type: scm.EnsurePreviewEnvironment, Repository: "acme/" + serviceName, PullRequest: 7, HeadSHA: "abc1234", Environment: environmentName, Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if duplicate, recordErr := primaryCommands.RecordSCMDelivery(scm.ProviderGitHub, suffix, command, now); recordErr != nil || duplicate {
		t.Fatalf("record command: duplicate=%v err=%v", duplicate, recordErr)
	}
	if _, err = primaryCommands.LeaseSCMCommand(now, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = primaryDB.Close(); err != nil {
		t.Fatal(err)
	}

	fake := &fakeOrchestrator{}
	replica := NewSCMCommandReconciler(secondaryState, secondaryCommands, fake, time.Hour, PreviewRuntimeConfig{ImageRepository: "registry.example.test/previews", BuilderImage: "buildkit:test", ScannerImage: "trivy:test", VulnerabilitySeverities: "CRITICAL", CosignImage: "cosign:test", CosignSigner: "awskms:///alias/preview-{tenant}", CosignPublicKeySecret: "cosign-public", SigningProfile: "kms", CosignAuthMode: "ambient", PolicyPredicateType: "https://example.test/policy/v1", TargetPlatform: "linux/amd64"}, zap.NewNop())
	if processed, processErr := replica.ProcessOne(context.Background(), now.Add(2*time.Second)); processErr != nil || !processed {
		t.Fatalf("replacement replica did not reconcile expired lease: processed=%v err=%v", processed, processErr)
	}
	if fake.creates != 1 || fake.deploys != 1 {
		t.Fatalf("replacement reconciliation was incomplete: creates=%d deploys=%d", fake.creates, fake.deploys)
	}
	if _, err = secondaryState.GetEnvironment(environmentName); err != nil {
		t.Fatalf("replacement replica did not persist environment: %v", err)
	}
}

type fakeOrchestrator struct {
	creates, deploys, destroys int
	deployStatus               *wf.WorkflowStatus
	lastDeployment             orchestrator.PreviewDeployment
}

func (f *fakeOrchestrator) Create(_ context.Context, spec orchestrator.EnvironmentSpec) (*orchestrator.Environment, error) {
	f.creates++
	return &orchestrator.Environment{Spec: spec, CreateWorkflow: orchestrator.WorkflowReference{Name: "create", Namespace: "argo"}}, nil
}
func (f *fakeOrchestrator) Destroy(_ context.Context, name, _, _ string) (*orchestrator.WorkflowReference, error) {
	f.destroys++
	return &orchestrator.WorkflowReference{Name: "destroy-" + name, Namespace: "argo"}, nil
}
func (f *fakeOrchestrator) Deploy(_ context.Context, _ *orchestrator.Environment, deployment orchestrator.PreviewDeployment) (*orchestrator.WorkflowReference, error) {
	f.deploys++
	f.lastDeployment = deployment
	return &orchestrator.WorkflowReference{Name: "deploy", Namespace: "argo"}, nil
}
func (*fakeOrchestrator) GetCreateStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return &wf.WorkflowStatus{Phase: wf.WorkflowSucceeded}, nil
}
func (*fakeOrchestrator) GetTTLStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return nil, nil
}
func (f *fakeOrchestrator) GetDeployStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return f.deployStatus, nil
}

func (f *fakeOrchestrator) GetDestroyStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return &wf.WorkflowStatus{Phase: wf.WorkflowSucceeded}, nil
}
func (*fakeOrchestrator) Ready(context.Context) error { return nil }

func TestReconcilerCreatesAndDestroysPreviewIdempotently(t *testing.T) {
	store := api.NewServiceStore()
	if err := store.Put(api.Service{Name: "checkout", RepoURL: "https://bitbucket.org/acme/checkout"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ensure := scm.LifecycleCommand{ID: "bitbucket:open", Provider: scm.ProviderBitbucket, DeliveryID: "open", Type: scm.EnsurePreviewEnvironment, Repository: "acme/checkout", PullRequest: 3, HeadSHA: "abc1234", Environment: "checkout-pr-3", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if _, err := store.RecordSCMDelivery(scm.ProviderBitbucket, "open", &ensure, now); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOrchestrator{}
	reconciler := NewSCMCommandReconciler(store, store, fake, time.Hour, PreviewRuntimeConfig{
		ImageRepository: "registry.example.test/previews", BuilderImage: "buildkit:test", RegistrySecretName: "registry-credentials",
		ScannerImage: "trivy:test", VulnerabilitySeverities: "CRITICAL", IgnoreUnfixed: true,
		CosignImage: "cosign:test", CosignSigner: "awskms:///alias/preview-{tenant}", SigningProfile: "kms", CosignPrivateKeySecret: "cosign-private", CosignPublicKeySecret: "cosign-public",
		CosignAuthMode: "ambient", VaultImage: "vault:test", VaultRole: "signer",
		PolicyPredicateType: "https://example.test/policy/v1",
		TargetPlatform:      "linux/amd64",
	}, zap.NewNop())
	if processed, err := reconciler.ProcessOne(context.Background(), now); err != nil || !processed {
		t.Fatalf("ensure: processed=%v err=%v", processed, err)
	}
	if fake.creates != 1 {
		t.Fatalf("expected one create, got %d", fake.creates)
	}
	if fake.deploys != 1 {
		t.Fatalf("expected one deployment, got %d", fake.deploys)
	}
	if fake.lastDeployment.ImageRef != "registry.example.test/previews/default/checkout:abc1234" || fake.lastDeployment.ImageRepository != "registry.example.test/previews/default/checkout" || fake.lastDeployment.PreviewURL != "http://preview.t-default-checkout-pr-3.svc.cluster.local:8080" {
		t.Fatalf("unexpected preview deployment: %#v", fake.lastDeployment)
	}
	env, err := store.GetEnvironment("checkout-pr-3")
	if err != nil {
		t.Fatal(err)
	}
	if env.Spec.Source.Repository != "acme/checkout" || env.Spec.Source.CloneURL != "https://bitbucket.org/acme/checkout" {
		t.Fatalf("unexpected source identity: %#v", env.Spec.Source)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	fake.deployStatus = &wf.WorkflowStatus{Phase: wf.WorkflowSucceeded, Outputs: &wf.Outputs{Parameters: []wf.Parameter{{Name: "image-digest", Value: wf.AnyStringPtr(digest)}}}}
	if err := reconciler.ObserveDeployments(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	env, _ = store.GetEnvironment("checkout-pr-3")
	if env.Spec.Source.DeployedSHA != "abc1234" || env.Spec.Source.DeployedImage != "registry.example.test/previews/default/checkout@"+digest || env.Spec.Source.ImageDigest != digest || env.Spec.Source.VulnerabilityPolicy != "passed" || env.Spec.Source.SignatureReference == "" || env.Spec.Source.PolicyAttestation == "" || env.Spec.Source.PreviewURL == "" || env.Spec.Source.DeploymentPhase != "Succeeded" {
		t.Fatalf("successful workflow was not promoted: %#v", env.Spec.Source)
	}
	update := ensure
	update.ID = "bitbucket:update"
	update.DeliveryID = "update"
	update.HeadSHA = "def5678"
	update.Status = scm.CommandPending
	if _, err := store.RecordSCMDelivery(scm.ProviderBitbucket, "update", &update, now); err != nil {
		t.Fatal(err)
	}
	if processed, err := reconciler.ProcessOne(context.Background(), now); err != nil || !processed {
		t.Fatalf("update: processed=%v err=%v", processed, err)
	}
	if fake.creates != 1 || fake.deploys != 2 {
		t.Fatalf("update should redeploy without reprovisioning: creates=%d deploys=%d", fake.creates, fake.deploys)
	}
	env, _ = store.GetEnvironment("checkout-pr-3")
	if env.Spec.Source.DesiredSHA != "def5678" || env.Spec.Source.DeployedSHA != "abc1234" || env.Spec.Source.DeploymentPhase != "Pending" {
		t.Fatalf("new generation should retain only the prior deployed SHA: %#v", env.Spec.Source)
	}
	fake.deployStatus = &wf.WorkflowStatus{Phase: wf.WorkflowFailed, Message: "build failed"}
	if err := reconciler.ObserveDeployments(context.Background(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	env, _ = store.GetEnvironment("checkout-pr-3")
	if env.Spec.Source.DeployedSHA != "abc1234" || env.Spec.Source.DeployedImage != "registry.example.test/previews/default/checkout@"+digest || env.Spec.Source.ImageDigest != digest || env.Spec.Source.VulnerabilityPolicy != "passed" || env.Spec.Source.DeploymentPhase != "Failed" || env.Spec.Source.DeploymentMessage != "build failed" {
		t.Fatalf("failed workflow must not promote its desired SHA: %#v", env.Spec.Source)
	}

	closeCommand := scm.LifecycleCommand{ID: "bitbucket:close", Provider: scm.ProviderBitbucket, DeliveryID: "close", Type: scm.DestroyPreviewEnvironment, Repository: "acme/checkout", PullRequest: 3, Environment: "checkout-pr-3", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if _, err := store.RecordSCMDelivery(scm.ProviderBitbucket, "close", &closeCommand, now); err != nil {
		t.Fatal(err)
	}
	if processed, err := reconciler.ProcessOne(context.Background(), now); err != nil || !processed {
		t.Fatalf("destroy: processed=%v err=%v", processed, err)
	}
	if fake.destroys != 1 {
		t.Fatalf("expected one destroy, got %d", fake.destroys)
	}
	commands := store.SCMCommands()
	for _, command := range commands {
		if command.Status != scm.CommandSucceeded {
			t.Fatalf("command not completed: %#v", command)
		}
	}
}

func TestDeploymentObservationFailsClosedWithoutDigest(t *testing.T) {
	store := api.NewServiceStore()
	env := &orchestrator.Environment{
		Spec: orchestrator.EnvironmentSpec{Name: "checkout-pr-9", Source: &orchestrator.SourceRevision{
			DesiredSHA: "abc1234", DesiredImage: "registry.test/checkout:abc1234", Generation: 1, DeploymentPhase: "Running",
		}},
		DeployWorkflow: &orchestrator.WorkflowReference{Name: "deploy-without-digest", Namespace: "argo"},
	}
	if err := store.PutEnvironment(env); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOrchestrator{deployStatus: &wf.WorkflowStatus{Phase: wf.WorkflowSucceeded}}
	reconciler := NewSCMCommandReconciler(store, store, fake, time.Hour, PreviewRuntimeConfig{}, zap.NewNop())
	if err := reconciler.ObserveDeployments(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	observed, _ := store.GetEnvironment("checkout-pr-9")
	if observed.Spec.Source.DeploymentPhase != "Error" || observed.Spec.Source.DeployedSHA != "" || observed.Spec.Source.DeployedImage != "" {
		t.Fatalf("digestless success escaped the fail-closed boundary: %#v", observed.Spec.Source)
	}
}

func TestPreviewDeploymentPartitionsTenantArtifactTrust(t *testing.T) {
	reconciler := NewSCMCommandReconciler(api.NewServiceStore(), api.NewServiceStore(), &fakeOrchestrator{}, time.Hour, PreviewRuntimeConfig{
		ImageRepository: "registry.example.test/previews", BuilderImage: "buildkit:test",
		ScannerImage: "trivy:test", VulnerabilitySeverities: "CRITICAL",
		CosignImage: "cosign:test", CosignSigner: "hashivault://preview-signing-{tenant}", SigningProfile: "kms",
		CosignPublicKeySecret: "preview-cosign-public", VEXConfigMap: "preview-vex-none",
		CosignAuthMode: "ambient", PolicyPredicateType: "https://example.test/policy/v1", TargetPlatform: "linux/amd64",
	}, zap.NewNop())
	service := api.Service{TenantID: "alpha", Name: "checkout", Repository: scm.RepositoryIdentity{Name: "checkout"}}

	deployment, err := reconciler.previewDeployment(service, "t-alpha-checkout-pr-3", "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ImageRepository != "registry.example.test/previews/alpha/checkout" || deployment.CosignSigner != "hashivault://preview-signing-alpha" {
		t.Fatalf("tenant image or signer escaped partitioning: %#v", deployment)
	}
	if deployment.CosignPublicKeySecret != "preview-cosign-public-alpha" || deployment.VEXConfigMap != "preview-vex-none-alpha" {
		t.Fatalf("tenant trust objects escaped partitioning: %#v", deployment)
	}
}

func TestEgressPolicyIsTenantScopedAndFailClosed(t *testing.T) {
	service := api.Service{
		TenantID: "alpha",
		Name:     "checkout",
		Deployment: api.ServiceDeployment{Egress: []api.ServiceEgressRule{
			{DNSName: "api.example.com", Port: 443, Protocol: "TCP"},
		}},
	}
	encoded, err := encodeEgressPolicy(service, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	metadata := manifest["metadata"].(map[string]any)
	labels := metadata["labels"].(map[string]any)
	if labels["platform.tenant"] != "alpha" || labels["platform.service"] != "checkout" {
		t.Fatalf("generated policy lost ownership labels: %#v", labels)
	}
	serialized := string(manifestBytes)
	for _, expected := range []string{`"matchName":"api.example.com"`, `"port":"443"`, `"protocol":"TCP"`} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("generated policy omitted %s: %s", expected, serialized)
		}
	}
	for _, forbidden := range []string{"0.0.0.0/0", "169.254.169.254", `"matchPattern"`} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("generated policy contains forbidden escape %q: %s", forbidden, serialized)
		}
	}

	deniedByDefault, err := encodeEgressPolicy(api.Service{Name: "closed"}, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	closedBytes, _ := base64.StdEncoding.DecodeString(deniedByDefault)
	if !strings.Contains(string(closedBytes), `"egress":[]`) {
		t.Fatalf("empty declaration did not render fail-closed egress: %s", closedBytes)
	}
}
