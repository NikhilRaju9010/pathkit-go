// Package timer_cancel starts a timer that the code can cancel.
package timer_cancel

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run waits on a timer, or cancels it when told to.
func Run(ctx workflow.Context, skip bool) error {
	timerCtx, cancel := workflow.WithCancel(ctx)
	t := workflow.NewTimer(timerCtx, time.Hour)
	if skip {
		cancel()
	}
	if err := t.Get(ctx, nil); err != nil {
		return err
	}
	return nil
}
