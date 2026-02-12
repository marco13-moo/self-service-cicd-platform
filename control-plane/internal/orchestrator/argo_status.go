package orchestrator

/*
import (
	"encoding/json"
	"os/exec"
	"time"
)

type argoGetResult struct {
	Status struct {
		Phase      string  `json:"phase"`
		StartedAt  *string `json:"startedAt"`
		FinishedAt *string `json:"finishedAt"`
	} `json:"status"`
}

// GetWorkflowStatus queries Argo for workflow status.
// Read-only. No state is stored.
func (a *ArgoExecutor) GetWorkflowStatus(
	ref WorkflowReference,
) (*WorkflowStatusView, error) {

	cmd := exec.Command(
		"argo",
		"get",
		ref.Name,
		"-n", ref.Namespace,
		"-o", "json",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result argoGetResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	view := &WorkflowStatusView{
		Phase: result.Status.Phase,
	}

	if result.Status.StartedAt != nil {
		if t, err := time.Parse(time.RFC3339, *result.Status.StartedAt); err == nil {
			view.StartedAt = &t
		}
	}
	if result.Status.FinishedAt != nil {
		if t, err := time.Parse(time.RFC3339, *result.Status.FinishedAt); err == nil {
			view.FinishedAt = &t
		}
	}

	return view, nil
}
*/
