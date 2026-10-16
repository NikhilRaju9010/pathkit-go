// Package helpererr is shape (g): a helper function that wraps an activity
// call and returns its error.
package helpererr

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func charge(ctx workflow.Context) error {
	return workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil)
}

// CATCH: the error of charge is the activity's error, so the check after it
// is a real two-way step (failure / success).
func HelperWrapsActivity(ctx workflow.Context) error {
	err := charge(ctx)
	if err != nil {
		return err
	}
	_ = act(ctx)
	return nil
}
