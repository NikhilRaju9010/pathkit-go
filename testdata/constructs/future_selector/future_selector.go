// Package future_selector waits for one of two futures with a workflow.Selector.
package future_selector

import "go.temporal.io/sdk/workflow"

// Run races two activities.
func Run(ctx workflow.Context) error {
	a := workflow.ExecuteActivity(ctx, "FastPath")
	b := workflow.ExecuteActivity(ctx, "SlowPath")
	var winner string
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(a, func(f workflow.Future) { winner = "a" })
	sel.AddFuture(b, func(f workflow.Future) { winner = "b" })
	sel.Select(ctx)
	if winner == "a" {
		return nil
	}
	return nil
}
