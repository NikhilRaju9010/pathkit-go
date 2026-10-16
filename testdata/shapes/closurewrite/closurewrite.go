// Package closurewrite is safety fixture (j): the variable is written inside
// a closure (workflow.Go, a selector callback, a defer). Must stay testable.
package closurewrite

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func WrittenInGo(ctx workflow.Context) error {
	var x string
	workflow.Go(ctx, func(ctx workflow.Context) {
		x = "set"
	})
	_ = act(ctx)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func WrittenInSelectorCallback(ctx workflow.Context) error {
	var x string
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Second), func(f workflow.Future) {
		x = "timer"
	})
	sel.Select(ctx)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func WrittenInDefer(ctx workflow.Context) error {
	var x string
	defer func() {
		x = "done"
	}()
	if x == "" {
		_ = act(ctx)
	}
	return nil
}
