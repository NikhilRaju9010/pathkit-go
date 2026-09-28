// Package clash already declares a name that pathkit's recorder needs.
package clash

import "go.temporal.io/sdk/workflow"

func pathkitStart() {}

func ClashWorkflow(ctx workflow.Context, x int) (string, error) {
	pathkitStart()
	if x > 0 {
		return "a", nil
	}
	return "b", nil
}
