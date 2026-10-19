// Package side is another package of the same module.
package side

import "go.temporal.io/sdk/workflow"

// Audit takes the workflow context and returns an error.
func Audit(ctx workflow.Context, id string) error {
	return workflow.Sleep(ctx, 0)
}
