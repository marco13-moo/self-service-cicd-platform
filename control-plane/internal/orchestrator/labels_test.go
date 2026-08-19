package orchestrator

import "testing"

func TestLabelBuilderBuildsEnvironmentWorkflowLabels(t *testing.T) {
	labels := NewLabelBuilder(WorkflowTypeEnvCreate, "payments").
		WithEnvironment("pr-42").
		WithTrigger(TriggerPR).
		WithTemplate("env-create-template").
		Build()

	want := map[string]string{
		LabelControlPlane:     "true",
		LabelExecutor:         "argo",
		LabelWorkflowType:     WorkflowTypeEnvCreate,
		LabelService:          "payments",
		LabelEnvironment:      "pr-42",
		LabelTrigger:          TriggerPR,
		LabelWorkflowTemplate: "env-create-template",
	}

	for key, value := range want {
		if labels[key] != value {
			t.Fatalf("label %q = %q, want %q", key, labels[key], value)
		}
	}
}

func TestLabelBuilderReturnsDefensiveCopy(t *testing.T) {
	builder := NewLabelBuilder(WorkflowTypeEnvTTL, "checkout")

	labels := builder.Build()
	labels[LabelService] = "mutated"

	next := builder.Build()
	if next[LabelService] != "checkout" {
		t.Fatalf("label builder leaked mutable labels: got service %q", next[LabelService])
	}
}
