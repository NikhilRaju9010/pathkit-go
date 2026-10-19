// Package future_unread starts an activity and never reads its result.
package future_unread

import "go.temporal.io/sdk/workflow"

// Run fires and forgets.
func Run(ctx workflow.Context, notify bool) error {
	if notify {
		f := workflow.ExecuteActivity(ctx, "Notify")
		_ = f
	}
	return nil
}
