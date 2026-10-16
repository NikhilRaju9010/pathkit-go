// Package recoverfn is safety fixture (n): a workflow that uses recover.
// Must stay testable.
package recoverfn

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

type In struct{ Flag bool }

func UsesRecover(ctx workflow.Context, in In) (err error) {
	s := ""
	defer func() {
		if r := recover(); r != nil {
			s = "recovered"
		}
	}()
	if in.Flag {
		_ = act(ctx)
	}
	if s == "" {
		_ = act(ctx)
	}
	return nil
}
