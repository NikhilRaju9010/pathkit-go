// Package errkind_nonretryable tells a non-retryable application error from a retryable one.
package errkind_nonretryable

import (
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Run branches on NonRetryable.
func Run(ctx workflow.Context) error {
	err := workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil)
	if err == nil {
		return nil
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		return err
	}
	return nil
}
