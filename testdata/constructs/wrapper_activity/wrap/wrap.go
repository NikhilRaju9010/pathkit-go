// Package wrap stands for a company library that runs activities.
package wrap

import "go.temporal.io/sdk/workflow"

// Run starts an activity by name and waits for it.
func Run(ctx workflow.Context, name string) error {
	return workflow.ExecuteActivity(ctx, name).Get(ctx, nil)
}
