// Package saga holds a small saga that PathKit's end-to-end test runs for
// real under "pathkit test": the compensation defer is a note on the
// paths that pass it, never a branch.
package saga

import (
	"context"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Cancellations counts how often the compensation ran (tests reset it).
var Cancellations atomic.Int32

// Reserve fails for good when the trip is "full".
func Reserve(ctx context.Context, trip string) error {
	if trip == "full" {
		return temporal.NewNonRetryableApplicationError("no seats", "Full", nil)
	}
	return nil
}

// Pay fails for good when the trip is "broke".
func Pay(ctx context.Context, trip string) error {
	if trip == "broke" {
		return temporal.NewNonRetryableApplicationError("card declined", "Declined", nil)
	}
	return nil
}

// CancelReservation is the compensation.
func CancelReservation(ctx context.Context, trip string) error {
	Cancellations.Add(1)
	return nil
}

// BookTripWorkflow reserves, then pays. Once the reservation exists, a
// later failure cancels it again (the deferred compensation).
func BookTripWorkflow(ctx workflow.Context, trip string) (err error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	if err = workflow.ExecuteActivity(ctx, Reserve, trip).Get(ctx, nil); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			dctx, _ := workflow.NewDisconnectedContext(ctx)
			_ = workflow.ExecuteActivity(dctx, CancelReservation, trip).Get(dctx, nil)
		}
	}()
	if err = workflow.ExecuteActivity(ctx, Pay, trip).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}
