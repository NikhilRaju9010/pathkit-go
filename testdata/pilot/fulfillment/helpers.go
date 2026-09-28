package fulfillment

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// These two take a workflow.Context but are NOT workflows. PathKit must not
// list them: newChildCtx is unexported, and AuditLog has no error result.

func newChildCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowExecutionTimeout: time.Hour,
	})
}

// AuditLog writes a line to the workflow's replay-safe logger.
func AuditLog(ctx workflow.Context, msg string) {
	workflow.GetLogger(ctx).Info(msg)
}
