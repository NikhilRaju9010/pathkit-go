// Package gotofn is safety fixture (n): a workflow that uses goto. PathKit
// does not map goto, so it must refuse with a clear message, never guess.
package gotofn

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

type In struct{ Flag bool }

func UsesGoto(ctx workflow.Context, in In) error {
	s := ""
	if in.Flag {
		goto fill
	}
	goto check
fill:
	s = "filled"
check:
	if s == "" {
		_ = act(ctx)
	}
	return nil
}
