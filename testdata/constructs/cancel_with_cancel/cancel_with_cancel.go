// Package cancel_with_cancel cancels a child context to stop an activity.
package cancel_with_cancel

import "go.temporal.io/sdk/workflow"

// Run starts an activity in a cancellable context and cancels it on request.
func Run(ctx workflow.Context, abort bool) error {
	child, cancel := workflow.WithCancel(ctx)
	f := workflow.ExecuteActivity(child, "LongJob")
	if abort {
		cancel()
	}
	return f.Get(ctx, nil)
}
