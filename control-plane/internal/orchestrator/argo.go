package orchestrator

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	argov1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	argoclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
)

/*
ArgoExecutor submits Workflows derived from WorkflowTemplates.

Phase 7 implementation:
- Uses Argo Go SDK
- Runs in-cluster using ServiceAccount identity
- Submits by WorkflowTemplate reference
- Captures workflow identity at submission time

Intentional limitations (unchanged):
- No retries
- No cancellation
- No reconciliation loop
- No inline YAML
*/

type ArgoExecutor struct {
	namespace string
	client    argoclient.Interface
}

func NewArgoExecutor(namespace string) *ArgoExecutor {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		panic(fmt.Errorf("failed to load in-cluster config: %w", err))
	}

	client, err := argoclient.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Errorf("failed to create argo client: %w", err))
	}

	return &ArgoExecutor{
		namespace: namespace,
		client:    client,
	}
}

// ---- public submission methods ----

// RunServiceCI selects a language-specific WorkflowTemplate
// and submits a Workflow derived from it.
func (a *ArgoExecutor) RunServiceCI(
	ctx context.Context,
	language string,
	params map[string]string,
) (*WorkflowReference, error) {

	templateName := "node-ci-template"
	if language == "python" {
		templateName = "python-ci-template"
	}

	return a.submitFromTemplate(ctx, templateName, params)
}

// submitFromTemplate submits a Workflow derived from a WorkflowTemplate
// and returns a WorkflowReference for observability.
func (a *ArgoExecutor) submitFromTemplate(
	ctx context.Context,
	templateName string,
	parameters map[string]string,
) (*WorkflowReference, error) {

	var params []argov1.Parameter
	for k, v := range parameters {
		params = append(params, argov1.Parameter{
			Name:  k,
			Value: argov1.AnyStringPtr(v),
		})
	}

	workflow := &argov1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: templateName + "-",
			Namespace:    a.namespace,
		},
		Spec: argov1.WorkflowSpec{
			WorkflowTemplateRef: &argov1.WorkflowTemplateRef{
				Name: templateName,
			},
			Arguments: argov1.Arguments{
				Parameters: params,
			},
		},
	}

	created, err := a.client.
		ArgoprojV1alpha1().
		Workflows(a.namespace).
		Create(ctx, workflow, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to submit workflow from template %q: %w",
			templateName,
			err,
		)
	}

	ref := &WorkflowReference{
		Name:        created.Name,
		Namespace:   created.Namespace,
		Template:    templateName,
		SubmittedAt: time.Now(),
	}

	return ref, nil
}
