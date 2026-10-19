// Package errkind_child_canceled tells a cancelled child workflow from a failed one.
package errkind_child_canceled

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Run starts a child and branches on cancellation.
func Run(ctx workflow.Context) error {
	err := workflow.ExecuteChildWorkflow(ctx, "Child").Get(ctx, nil)
	if err != nil {
		if temporal.IsCanceledError(err) {
			return nil
		}
		return err
	}
	return nil
}
