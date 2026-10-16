// Package loopchange is safety fixture (l): a variable changed in the loop
// body is checked at the loop head. Must stay testable.
package loopchange

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func ChangedInBody(ctx workflow.Context, finished bool) error {
	done := finished
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

func CountedLoop(ctx workflow.Context, limit int) error {
	n := 0
	for n < limit {
		_ = workflow.Sleep(ctx, time.Second)
		n++
	}
	return nil
}
