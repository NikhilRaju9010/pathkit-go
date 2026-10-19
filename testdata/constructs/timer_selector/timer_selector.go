// Package timer_selector races a signal against a timer in a workflow.Selector.
package timer_selector

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run waits for a signal or a timeout.
func Run(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "go")
	timer := workflow.NewTimer(ctx, time.Hour)
	sel := workflow.NewSelector(ctx)
	got := false
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) { got = true })
	sel.AddFuture(timer, func(f workflow.Future) { got = false })
	sel.Select(ctx)
	if !got {
		return workflow.ErrDeadlineExceeded
	}
	return nil
}
