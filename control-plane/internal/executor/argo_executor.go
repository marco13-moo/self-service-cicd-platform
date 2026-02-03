package executor

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	argov1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	argoclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
)

type ArgoExecutor struct {
	namespace string
	client    argoclient.Interface
}

func NewArgoExecutor(namespace string) (*ArgoExecutor, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster kube config: %w", err)
	}

	client, err := argoclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create argo client: %w", err)
	}

	return &ArgoExecutor{
		namespace: namespace,
		client:    client,
	}, nil
}

func (e *ArgoExecutor) SubmitWorkflowTemplate(
	templateName string,
	parameters map[string]string,
) (string, error) {

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
			Namespace:    e.namespace,
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

	created, err := e.client.
		ArgoprojV1alpha1().
		Workflows(e.namespace).
		Create(context.Background(), workflow, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("submit workflow template %q: %w", templateName, err)
	}

	return created.Name, nil
}
