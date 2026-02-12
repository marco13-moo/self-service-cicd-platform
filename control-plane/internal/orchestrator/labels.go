/*
package orchestrator

func BaseWorkflowLabels(

	workflowType string,
	service string,

) map[string]string {

		return map[string]string{
			"platform.workflow.type": workflowType,
			"platform.service":       service,
		}
	}
*/
package orchestrator

//
// Platform Label Keys
//
// NEVER change these casually.
// Treat them as part of your control-plane contract.
//

const (
	LabelControlPlane     = "platform.control-plane"
	LabelExecutor         = "platform.executor"
	LabelWorkflowType     = "platform.workflow.type"
	LabelService          = "platform.service"
	LabelEnvironment      = "platform.environment"
	LabelTrigger          = "platform.trigger"
	LabelWorkflowTemplate = "platform.workflow.template"
)

//
// WorkflowTypes
//

const (
	WorkflowTypeEnvCreate  = "environment-create"
	WorkflowTypeEnvDestroy = "environment-destroy"
	WorkflowTypeEnvTTL     = "environment-ttl"
)

//
// Triggers
//

const (
	TriggerAPI    = "api"
	TriggerSystem = "system"
	TriggerPR     = "pull_request" // Phase 8 ready
)

//
// LabelBuilder enforces schema correctness.
//

type LabelBuilder struct {
	labels map[string]string
}

func NewLabelBuilder(
	workflowType string,
	service string,
) *LabelBuilder {

	return &LabelBuilder{
		labels: map[string]string{
			LabelControlPlane: "true",
			LabelExecutor:     "argo",
			LabelWorkflowType: workflowType,
			LabelService:      service,
		},
	}
}

func (b *LabelBuilder) WithEnvironment(env string) *LabelBuilder {
	b.labels[LabelEnvironment] = env
	return b
}

func (b *LabelBuilder) WithTrigger(trigger string) *LabelBuilder {
	b.labels[LabelTrigger] = trigger
	return b
}

func (b *LabelBuilder) WithTemplate(template string) *LabelBuilder {
	b.labels[LabelWorkflowTemplate] = template
	return b
}

//
// Build returns a defensive copy.
//
// Prevents mutation after submission.
//

func (b *LabelBuilder) Build() map[string]string {

	out := make(map[string]string, len(b.labels))

	for k, v := range b.labels {
		out[k] = v
	}

	return out
}
