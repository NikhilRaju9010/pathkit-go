// Package signal_timeout waits for a signal for a limited time.
package signal_timeout

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run receives with a timeout.
func Run(ctx workflow.Context) error {
	var msg string
	ok, _ := workflow.GetSignalChannel(ctx, "msg").ReceiveWithTimeout(ctx, time.Hour, &msg)
	if !ok {
		return workflow.ErrDeadlineExceeded
	}
	return nil
}
