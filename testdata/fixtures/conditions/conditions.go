// Package conditions has decisions whose condition is a chain of && and ||,
// to test the "each part of a condition" counts. All names are made up.
package conditions

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Request is a made-up order request.
type Request struct {
	Amount  int
	Kind    string
	Express bool
	Region  string
}

// Flag is a named bool type: a part of this type must still be recorded.
type Flag bool

// Ship is an activity.
func Ship(_ context.Context, _ Request) error { return nil }

func opts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// Chain has one decision with four parts.
func Chain(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	if req.Amount > 0 && (req.Kind == "A" || req.Express) && req.Region != "" {
		return workflow.ExecuteActivity(ctx, Ship, req).Get(ctx, nil)
	}
	return nil
}

// Single has a condition that is one part: no parts to count.
func Single(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	if req.Express {
		return workflow.ExecuteActivity(ctx, Ship, req).Get(ctx, nil)
	}
	return nil
}

func isOK(b bool) bool { return b }

// Repeated has the same part twice in one decision.
func Repeated(ctx workflow.Context, ok bool) error {
	if isOK(ok) || isOK(ok) {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

func inRange(ctx workflow.Context, n int) error {
	if n > 1 && n < 10 {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// HelperAnd calls a helper twice; the helper's decision has one set of parts.
func HelperAnd(ctx workflow.Context, a, b int) error {
	if err := inRange(ctx, a); err != nil {
		return err
	}
	return inRange(ctx, b)
}

// Looped has a loop whose condition is a chain, with a named bool part.
func Looped(ctx workflow.Context, n int, stop Flag) error {
	for i := 0; i < n && !bool(stop) || i == 0 && n < 0; i++ {
		if err := workflow.Sleep(ctx, time.Second); err != nil {
			return err
		}
	}
	return nil
}

// Named has a part of a named bool type.
func Named(ctx workflow.Context, f Flag, n int) error {
	if f && n > 1 {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}
