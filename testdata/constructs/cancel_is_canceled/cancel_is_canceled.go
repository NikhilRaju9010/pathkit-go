// Package cancel_is_canceled checks an error for cancellation.
package cancel_is_canceled

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Run sleeps and tells cancellation apart from other errors.
func Run(ctx workflow.Context) error {
	if err := workflow.Sleep(ctx, time.Hour); err != nil {
		if temporal.IsCanceledError(err) {
			return nil
		}
		return err
	}
	return nil
}
