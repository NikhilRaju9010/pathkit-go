// Package signal_async polls a signal channel without waiting.
package signal_async

import "go.temporal.io/sdk/workflow"

// Run checks once whether a signal is waiting.
func Run(ctx workflow.Context) error {
	var msg string
	if workflow.GetSignalChannel(ctx, "msg").ReceiveAsync(&msg) {
		return nil
	}
	return workflow.ErrCanceled
}
