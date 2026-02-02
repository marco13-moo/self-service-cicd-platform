package orchestrator

import (
	"encoding/json"
	"os/exec"
)

type argoGetResult struct {
	Status struct {
		Phase      string  `json:"phase"`
		StartedAt  *string `json:"startedAt"`
		FinishedAt *string `json:"finishedAt"`
	} `json:"status"`
}

func (a *ArgoClient) GetWorkflowStatus(ref WorkflowReference) (*WorkflowStatusView, error) {
	cmd := exec.Command("argo", "get", ref.Name, "-n", ref.Namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result argoGetResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	return &WorkflowStatusView{
		Phase: result.Status.Phase,
	}, nil
}
