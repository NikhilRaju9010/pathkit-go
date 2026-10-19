// Package future_later starts an activity, does other work, and reads the
// result later: the step happens where the future is read.
package future_later

import "go.temporal.io/sdk/workflow"

// Run stores the future, checks a flag, then reads the future.
func Run(ctx workflow.Context, quick bool) error {
	f := workflow.ExecuteActivity(ctx, "Charge")
	if quick {
		return nil
	}
	if err := f.Get(ctx, nil); err != nil {
		return err
	}
	return nil
}
