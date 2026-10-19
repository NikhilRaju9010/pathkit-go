// Package returncall has workflows that return the error of a Temporal call
// directly: "return fut.Get(...)", "return workflow.Sleep(...)". The call
// fails or succeeds, and the workflow ends failed or completed.
package returncall

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// opts lets an activity run once, with a time limit.
func opts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
}

// ReturnGet returns the error of an activity's result.
func ReturnGet(ctx workflow.Context) error {
	ctx = opts(ctx)
	return workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil)
}

// ReturnSleep returns the error of a timer.
func ReturnSleep(ctx workflow.Context, wait bool) error {
	if !wait {
		return nil
	}
	return workflow.Sleep(ctx, time.Millisecond)
}

// ReturnWithValue returns a value and the error of a read.
func ReturnWithValue(ctx workflow.Context) (string, error) {
	ctx = opts(ctx)
	var out string
	f := workflow.ExecuteActivity(ctx, "Charge")
	return "done", f.Get(ctx, &out)
}

// ReturnHelper returns the result of a helper: not one of these shapes.
func ReturnHelper(ctx workflow.Context) error {
	return charge(ctx)
}

func charge(ctx workflow.Context) error {
	ctx = opts(ctx)
	if err := workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil); err != nil {
		return err
	}
	return nil
}
