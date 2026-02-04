package executor

import (
	"context"
	"fmt"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ArgoSDKExecutor struct {
	clients   *Clients
	namespace string
}

func NewArgoSDKExecutor(
	clients *Clients,
	namespace string,
) *ArgoSDKExecutor {
	return &ArgoSDKExecutor{
		clients:   clients,
		namespace: namespace,
	}
}

func (e *ArgoSDKExecutor) SubmitFromTemplate(
	ctx context.Context,
	templateName string,
	generateName string,
	params map[string]string,
) (*wf.Workflow, error) {

	args := wf.Arguments{}

	for k, v := range params {
		args.Parameters = append(args.Parameters,
			wf.Parameter{
				Name:  k,
				Value: wf.AnyStringPtr(v),
			})
	}

	workflow := &wf.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
		},
		Spec: wf.WorkflowSpec{
			WorkflowTemplateRef: &wf.WorkflowTemplateRef{
				Name: templateName,
			},
			Arguments: args,
		},
	}

	created, err := e.clients.
		Argo.
		ArgoprojV1alpha1().
		Workflows(e.namespace).
		Create(ctx, workflow, metav1.CreateOptions{})

	if err != nil {
		return nil, fmt.Errorf("submit workflow: %w", err)
	}

	return created, nil
}
