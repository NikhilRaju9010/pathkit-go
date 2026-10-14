// Package zeropaths holds a workflow that has no path at all (a loop with no
// way out), next to an ordinary one. PathKit warns about the first and says
// nothing new about the second.
package zeropaths

import "go.temporal.io/sdk/workflow"

// Spin never finishes (a loop with no break and no return): PathKit lists no path for it.
func Spin(ctx workflow.Context) error {
	n := 0
	for {
		n++
	}
}

// Plain has one decision and so two paths.
func Plain(ctx workflow.Context, ok bool) error {
	if !ok {
		return nil
	}
	return nil
}
