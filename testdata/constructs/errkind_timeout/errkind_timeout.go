// Package errkind_timeout tells a timeout from other failures.
package errkind_timeout

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Run branches on a timeout.
func Run(ctx workflow.Context) error {
	err := workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil)
	if err != nil {
		if temporal.IsTimeoutError(err) {
			return nil
		}
		return err
	}
	return nil
}
