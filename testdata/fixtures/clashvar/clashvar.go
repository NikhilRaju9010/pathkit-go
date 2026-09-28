// Package clashvar has a workflow that already uses the variable name
// pathkit inserts.
package clashvar

import "go.temporal.io/sdk/workflow"

func ClashVarWorkflow(ctx workflow.Context, x int) (string, error) {
	pathkitRec := x * 2
	if pathkitRec > 0 {
		return "a", nil
	}
	return "b", nil
}
