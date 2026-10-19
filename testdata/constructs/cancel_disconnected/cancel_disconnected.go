// Package cancel_disconnected cleans up in a disconnected context after the
// workflow was cancelled.
package cancel_disconnected

import "go.temporal.io/sdk/workflow"

// Run does the work, and after a failure runs a cleanup that survives cancellation.
func Run(ctx workflow.Context) error {
	if err := workflow.ExecuteActivity(ctx, "Work").Get(ctx, nil); err != nil {
		cleanup, cancel := workflow.NewDisconnectedContext(ctx)
		defer cancel()
		_ = workflow.ExecuteActivity(cleanup, "Cleanup").Get(cleanup, nil)
		return err
	}
	return nil
}
