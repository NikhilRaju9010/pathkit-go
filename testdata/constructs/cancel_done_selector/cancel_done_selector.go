// Package cancel_done_selector waits for a signal or for the workflow's
// cancellation, with ctx.Done() as a selector case.
package cancel_done_selector

import "go.temporal.io/sdk/workflow"

// Run waits for either.
func Run(ctx workflow.Context) error {
	sel := workflow.NewSelector(ctx)
	cancelled := false
	sel.AddReceive(workflow.GetSignalChannel(ctx, "go"), func(c workflow.ReceiveChannel, more bool) {})
	sel.AddReceive(ctx.Done(), func(c workflow.ReceiveChannel, more bool) { cancelled = true })
	sel.Select(ctx)
	if cancelled {
		return workflow.ErrCanceled
	}
	return nil
}
