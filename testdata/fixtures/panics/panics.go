// Package panics proves that under pathkit's overlay a panic still points
// at the real file and line. Several lines above the panic get recording
// code inserted (the function's first line, the if's line), and so does
// the panic's own line (before its return), so any shift would show.
package panics

import "go.temporal.io/sdk/workflow"

func PanicWorkflow(ctx workflow.Context, x int) (string, error) {
	if x > 0 {
		return explode(x), nil // the workflow's frame is on this line
	}
	return "ok", nil
}

func explode(x int) string { panic("boom") }
