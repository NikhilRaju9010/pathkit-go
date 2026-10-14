// Package rulewithout holds workflows for the rule format that has a
// "without" part, and for decisions with the same text.
package rulewithout

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func withOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// ListWorkflow builds one future per item and then waits for every future:
// the second loop is empty exactly when the first one is.
func ListWorkflow(ctx workflow.Context, items []string) error {
	ctx = withOptions(ctx)
	var futures []workflow.Future
	for _, it := range items {
		futures = append(futures, workflow.ExecuteActivity(ctx, "send", it))
	}
	for _, f := range futures {
		if err := f.Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}

// TwiceWorkflow has two decisions with exactly the same text.
func TwiceWorkflow(ctx workflow.Context, a, b bool) error {
	ctx = withOptions(ctx)
	if a || b {
		if err := workflow.ExecuteActivity(ctx, "first").Get(ctx, nil); err != nil {
			return err
		}
	}
	if a || b {
		if err := workflow.ExecuteActivity(ctx, "second").Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}
