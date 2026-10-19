// Package summarize has workflows that call one helper at many places. Copying
// the helper's decisions into the map at every call makes the number of paths
// grow with the calls; mapping the helper once does not.
package summarize

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// pause is a helper with a decision of its own and a Temporal call.
func pause(ctx workflow.Context, fast bool) error {
	if fast {
		return nil
	}
	return workflow.Sleep(ctx, time.Millisecond)
}

// Chain calls pause at twelve places: with the helper copied in, it has more
// paths than the path cap.
func Chain(ctx workflow.Context, in [12]bool) error {
	if err := pause(ctx, in[0]); err != nil {
		return err
	}
	if err := pause(ctx, in[1]); err != nil {
		return err
	}
	if err := pause(ctx, in[2]); err != nil {
		return err
	}
	if err := pause(ctx, in[3]); err != nil {
		return err
	}
	if err := pause(ctx, in[4]); err != nil {
		return err
	}
	if err := pause(ctx, in[5]); err != nil {
		return err
	}
	if err := pause(ctx, in[6]); err != nil {
		return err
	}
	if err := pause(ctx, in[7]); err != nil {
		return err
	}
	if err := pause(ctx, in[8]); err != nil {
		return err
	}
	if err := pause(ctx, in[9]); err != nil {
		return err
	}
	if err := pause(ctx, in[10]); err != nil {
		return err
	}
	if err := pause(ctx, in[11]); err != nil {
		return err
	}
	return nil
}

// Small calls pause twice: it stays well under the cap, so it keeps its
// helper copied in (every path through the helper is listed).
func Small(ctx workflow.Context, a, b bool) error {
	if err := pause(ctx, a); err != nil {
		return err
	}
	return pause(ctx, b)
}

// ErrorKinds: the helper returns two different errors and the caller does not
// tell them apart; the caller's check is one decision.
func notify(ctx workflow.Context, n int) error {
	if n < 0 {
		return workflow.Sleep(ctx, 0)
	}
	return workflow.Sleep(ctx, time.Millisecond)
}

// Nested calls a helper that calls a helper.
func outer(ctx workflow.Context, fast bool) error {
	if err := pause(ctx, fast); err != nil {
		return err
	}
	return notify(ctx, 1)
}

// Nested has the helper chain outer -> pause and notify.
func Nested(ctx workflow.Context, fast bool) error {
	return outer(ctx, fast)
}

// Looping calls a helper that has a loop.
func spin(ctx workflow.Context, n int) error {
	for i := 0; i < n; i++ {
		if err := workflow.Sleep(ctx, time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

// Looping runs the helper with a loop.
func Looping(ctx workflow.Context, n int) error {
	if err := spin(ctx, n); err != nil {
		return err
	}
	return spin(ctx, 1)
}
