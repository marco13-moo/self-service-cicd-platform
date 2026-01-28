package executor

/*
Phase 4 placeholder: Argo SDK executor

This file intentionally does NOT submit workflows yet.

Rationale:
- Argo Go SDK is unstable and poorly documented
- Control plane currently runs out-of-cluster
- CLI-based submission is sufficient to validate execution-plane boundaries

This executor will be activated in a later phase once:
- Control plane runs in-cluster
- Workflow status tracking is required
- Retries, cancellation, and auth are needed
*/

type ArgoExecutor struct {
	namespace string
}

func NewArgoExecutor(namespace string) (*ArgoExecutor, error) {
	return &ArgoExecutor{
		namespace: namespace,
	}, nil
}
