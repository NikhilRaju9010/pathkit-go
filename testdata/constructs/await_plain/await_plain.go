// Package await_plain waits for a condition with workflow.Await.
package await_plain

import "go.temporal.io/sdk/workflow"

// Run waits until approved, and checks the error.
func Run(ctx workflow.Context) error {
	approved := false
	workflow.Go(ctx, func(ctx workflow.Context) {
		workflow.GetSignalChannel(ctx, "approve").Receive(ctx, nil)
		approved = true
	})
	if err := workflow.Await(ctx, func() bool { return approved }); err != nil {
		return err
	}
	return nil
}
