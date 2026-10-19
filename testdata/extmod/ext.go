// Package ext stands for a library from another module: a function that
// takes a workflow.Context and whose code PathKit does not read.
package ext

import "go.temporal.io/sdk/workflow"

// Notify does something in the workflow.
func Notify(ctx workflow.Context, id string) error {
	return workflow.Sleep(ctx, 0)
}
