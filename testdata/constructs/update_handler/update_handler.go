// Package update_handler registers an update handler that runs an activity.
package update_handler

import "go.temporal.io/sdk/workflow"

// Run registers the handler and waits for a stop signal.
func Run(ctx workflow.Context) error {
	count := 0
	err := workflow.SetUpdateHandler(ctx, "add", func(ctx workflow.Context, n int) (int, error) {
		if n < 0 {
			return count, nil
		}
		if err := workflow.ExecuteActivity(ctx, "Record", n).Get(ctx, nil); err != nil {
			return count, err
		}
		count += n
		return count, nil
	})
	if err != nil {
		return err
	}
	workflow.GetSignalChannel(ctx, "stop").Receive(ctx, nil)
	return nil
}
