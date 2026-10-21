// Package cleanup has a workflow that cleans up under a disconnected context
// after its work failed or was cancelled. All names are made up.
package cleanup

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run does the work; when it fails, a cleanup that survives a cancel runs, and
// its own failure is checked.
func Run(ctx workflow.Context, big bool) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	if err := workflow.ExecuteActivity(ctx, "Work").Get(ctx, nil); err != nil {
		dctx, cancel := workflow.NewDisconnectedContext(ctx)
		defer cancel()
		if big {
			_ = workflow.ExecuteActivity(dctx, "Notify").Get(dctx, nil)
		}
		if cerr := workflow.ExecuteActivity(dctx, "Cleanup").Get(dctx, nil); cerr != nil {
			return cerr
		}
		return err
	}
	return nil
}
