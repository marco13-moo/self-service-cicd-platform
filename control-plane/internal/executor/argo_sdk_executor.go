package executor

import (
	"context"
	"fmt"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Compile-time enforcement.
// If the interface changes, this fails the build immediately.
var _ WorkflowExecutor = (*ArgoSDKExecutor)(nil)

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
	parameters map[string]string,
	labels map[string]string, // <-- REQUIRED by the upgraded interface
) (*wf.Workflow, error) {

	//-----------------------------------------
	// Build parameters
	//-----------------------------------------

	args := wf.Arguments{}

	for k, v := range parameters {
		val := v // avoid pointer aliasing
		args.Parameters = append(args.Parameters,
			wf.Parameter{
				Name:  k,
				Value: wf.AnyStringPtr(val),
			})
	}

	//-----------------------------------------
	// Build labels OUTSIDE the struct literal
	//-----------------------------------------

	mergedLabels := map[string]string{
		"platform.control-plane":     "true",
		"platform.executor":          "argo",
		"platform.workflow.template": templateName,
	}

	// Caller labels override ONLY non-platform keys.
	for k, v := range labels {
		mergedLabels[k] = v
	}

	//-----------------------------------------
	// Construct workflow
	//-----------------------------------------

	workflow := &wf.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
			Labels:       mergedLabels,
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
		return nil, fmt.Errorf("submit workflow from template %s: %w", templateName, err)
	}

	return created, nil
}

func (e *ArgoSDKExecutor) GetWorkflow(
	ctx context.Context,
	name string,
) (*wf.Workflow, error) {

	w, err := e.clients.
		Argo.
		ArgoprojV1alpha1().
		Workflows(e.namespace).
		Get(ctx, name, metav1.GetOptions{})

	if err != nil {
		return nil, fmt.Errorf("get workflow %s: %w", name, err)
	}

	return w, nil
}

func (e *ArgoSDKExecutor) Cancel(
	ctx context.Context,
	name string,
) error {

	patch := []byte(`{"spec":{"shutdown":"Terminate"}}`)

	_, err := e.clients.
		Argo.
		ArgoprojV1alpha1().
		Workflows(e.namespace).
		Patch(
			ctx,
			name,
			types.MergePatchType,
			patch,
			metav1.PatchOptions{},
		)

	if err != nil {
		return fmt.Errorf("cancel workflow %s: %w", name, err)
	}

	return nil
}
