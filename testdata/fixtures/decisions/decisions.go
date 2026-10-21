// Package decisions has two workflows with the same decisions: one is tested
// by a test that checks the result, the other by a test that runs it and
// checks nothing. All names are made up.
package decisions

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Order is a made-up order.
type Order struct {
	Amount int
}

// Notify is an activity.
func Notify(_ context.Context, _ Order) error { return nil }

func opts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// Checked says "big" or "small" and fails when the notice fails.
func Checked(ctx workflow.Context, o Order) (string, error) {
	ctx = opts(ctx)
	kind := "small"
	if o.Amount > 100 {
		kind = "big"
	}
	if err := workflow.ExecuteActivity(ctx, Notify, o).Get(ctx, nil); err != nil {
		return "", err
	}
	return kind, nil
}

// Unchecked does the same, tested by a test that checks nothing.
func Unchecked(ctx workflow.Context, o Order) (string, error) {
	ctx = opts(ctx)
	kind := "small"
	if o.Amount > 100 {
		kind = "big"
	}
	if err := workflow.ExecuteActivity(ctx, Notify, o).Get(ctx, nil); err != nil {
		return "", err
	}
	return kind, nil
}
