package executor

import (
	"context"
	"testing"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	argofake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
)

func TestArgoSDKExecutorSubmitFromTemplateCreatesWorkflowIntent(t *testing.T) {
	client := argofake.NewSimpleClientset()
	exec := NewArgoSDKExecutor(&Clients{Argo: client}, "argo")

	created, err := exec.SubmitFromTemplate(
		context.Background(),
		"env-create-template",
		"env-create-",
		map[string]string{
			"env_name":   "pr-42",
			"service":    "payments",
			"expires_at": "2026-08-18T17:00:00Z",
		},
		map[string]string{
			"platform.executor":      "spoofed",
			"platform.workflow.type": "environment-create",
			"platform.service":       "payments",
		},
	)
	if err != nil {
		t.Fatalf("SubmitFromTemplate returned error: %v", err)
	}

	if created.Namespace != "argo" {
		t.Fatalf("namespace = %q, want argo", created.Namespace)
	}
	if created.GenerateName != "env-create-" {
		t.Fatalf("generateName = %q, want env-create-", created.GenerateName)
	}
	if created.Spec.WorkflowTemplateRef == nil {
		t.Fatal("workflow template ref is nil")
	}
	if created.Spec.WorkflowTemplateRef.Name != "env-create-template" {
		t.Fatalf("template ref = %q, want env-create-template", created.Spec.WorkflowTemplateRef.Name)
	}

	if created.Labels["platform.executor"] != "argo" {
		t.Fatalf("executor label = %q, want argo", created.Labels["platform.executor"])
	}
	if created.Labels["platform.control-plane"] != "true" {
		t.Fatalf("control-plane label = %q, want true", created.Labels["platform.control-plane"])
	}
	if created.Labels["platform.workflow.template"] != "env-create-template" {
		t.Fatalf("template label = %q, want env-create-template", created.Labels["platform.workflow.template"])
	}

	assertParameter(t, created.Spec.Arguments.Parameters, "env_name", "pr-42")
	assertParameter(t, created.Spec.Arguments.Parameters, "service", "payments")
	assertParameter(t, created.Spec.Arguments.Parameters, "expires_at", "2026-08-18T17:00:00Z")
}

func assertParameter(t *testing.T, params []wf.Parameter, name string, want string) {
	t.Helper()

	for _, param := range params {
		if param.Name == name {
			if param.Value == nil {
				t.Fatalf("parameter %q has nil value", name)
			}
			if param.Value.String() != want {
				t.Fatalf("parameter %q = %q, want %q", name, param.Value.String(), want)
			}
			return
		}
	}

	t.Fatalf("missing parameter %q", name)
}
