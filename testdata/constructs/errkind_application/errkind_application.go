// Package errkind_application tells an application error from other failures.
package errkind_application

import (
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Run branches on whether the failure is an application error.
func Run(ctx workflow.Context) error {
	err := workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil)
	if err == nil {
		return nil
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return nil
	}
	return err
}
