// Package scenariotraps has shapes that once made scenarios wrong: an activity
// chosen by a variable, and a workflow with no decision at all.
package scenariotraps

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Charge and Refund do nothing.
func Charge(context.Context) error { return nil }
func Refund(context.Context) error { return nil }

// Input is what the workflows start with.
type Input struct{ Back bool }

// Variable calls Charge or Refund, chosen through a variable: its two paths
// are not the same behaviour and must not be merged.
func Variable(ctx workflow.Context, in Input) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	act := Charge
	if in.Back {
		act = Refund
	}
	if err := workflow.ExecuteActivity(ctx, act).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Straight has no decision: one path, no step in it.
func Straight(ctx workflow.Context, n int) error {
	return workflow.Sleep(ctx, time.Second)
}
