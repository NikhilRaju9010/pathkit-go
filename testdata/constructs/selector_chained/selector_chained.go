// Package selector_chained builds a workflow.Selector and uses it in one chained
// expression.
package selector_chained

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run waits for a signal or a timeout, with the selector written as one chain.
func Run(ctx workflow.Context) error {
	got := false
	workflow.NewSelector(ctx).
		AddReceive(workflow.GetSignalChannel(ctx, "go"), func(c workflow.ReceiveChannel, more bool) { got = true }).
		AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) { got = false }).
		Select(ctx)
	if !got {
		return workflow.ErrDeadlineExceeded
	}
	return nil
}
