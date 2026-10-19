// Package await_timeout waits for a condition with a timeout.
package await_timeout

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run waits up to a day for approval.
func Run(ctx workflow.Context) error {
	approved := false
	workflow.Go(ctx, func(ctx workflow.Context) {
		workflow.GetSignalChannel(ctx, "approve").Receive(ctx, nil)
		approved = true
	})
	ok, err := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool { return approved })
	if err != nil {
		return err
	}
	if !ok {
		return workflow.ErrDeadlineExceeded
	}
	return nil
}
