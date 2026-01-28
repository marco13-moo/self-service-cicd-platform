package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

/*
ArgoExecutor submits Workflows derived from WorkflowTemplates.

Phase 4 intent:
- Treat Argo as an external execution plane
- Submit by reference (WorkflowTemplate), not inline YAML
- Use the Argo CLI to avoid early SDK coupling

Intentional limitations:
- Requires `argo` binary on PATH
- Requires kubeconfig with access to target namespace
- No retries, status tracking, or cancellation
- No dynamic template discovery
*/

type ArgoExecutor struct {
	namespace string
}

func NewArgoExecutor(namespace string) *ArgoExecutor {
	return &ArgoExecutor{
		namespace: namespace,
	}
}

// RunServiceCI selects a language-specific WorkflowTemplate
// and submits a Workflow derived from it.
func (a *ArgoExecutor) RunServiceCI(
	ctx context.Context,
	language string,
	params map[string]string,
) error {

	templateName := "node-ci-template"
	if language == "python" {
		templateName = "python-ci-template"
	}

	return a.submitFromTemplate(ctx, templateName, params)
}

// submitFromTemplate mirrors:
//
//	argo submit -n <ns> --from workflowtemplate/<name> -p k=v
func (a *ArgoExecutor) submitFromTemplate(
	ctx context.Context,
	templateName string,
	parameters map[string]string,
) error {

	args := []string{
		"submit",
		"-n", a.namespace,
		"--from", fmt.Sprintf("workflowtemplate/%s", templateName),
	}

	for k, v := range parameters {
		args = append(args, "-p", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "argo", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("argo submit failed: %w: %s", err, stderr.String())
	}

	return nil
}
