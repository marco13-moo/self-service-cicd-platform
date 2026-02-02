package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

/*
ArgoExecutor submits Workflows derived from WorkflowTemplates.

Phase 4 intent:
- Treat Argo as an external execution plane
- Submit by reference (WorkflowTemplate), not inline YAML
- Use the Argo CLI to avoid early SDK coupling

Phase 6 extension:
- Capture workflow identity at submission time
- Return immutable workflow references for observability

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

// ---- internal CLI result types ----

// argoSubmitResult captures the minimal metadata returned by
// `argo submit -o json`
type argoSubmitResult struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
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

// submitFromTemplate mirrors:
//
//	argo submit -n <ns> --from workflowtemplate/<name> -p k=v -o json
//
// and returns a WorkflowReference for observability.
func (a *ArgoExecutor) submitFromTemplate(
	ctx context.Context,
	templateName string,
	parameters map[string]string,
) (*WorkflowReference, error) {

	args := []string{
		"submit",
		"-n", a.namespace,
		"--from", fmt.Sprintf("workflowtemplate/%s", templateName),
		"-o", "json",
	}

	for k, v := range parameters {
		args = append(args, "-p", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "argo", args...)

	// 🔍 PHASE-6 DIAGNOSTIC (INTENTIONAL)
	// This prints the *exact* CLI invocation so we can see
	// namespace, template, and parameters with zero ambiguity.
	fmt.Println("ARGO SUBMIT:", strings.Join(cmd.Args, " "))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(
			"argo submit failed: %w: %s",
			err,
			stderr.String(),
		)
	}

	var result argoSubmitResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf(
			"failed to parse argo submit output: %w",
			err,
		)
	}

	ref := &WorkflowReference{
		Name:        result.Metadata.Name,
		Namespace:   result.Metadata.Namespace,
		Template:    templateName, // ✅ FIXED (was incorrectly hardcoded)
		SubmittedAt: time.Now(),
	}

	return ref, nil
}
