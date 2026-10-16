// Package zerovalue is shape (f): a variable declared and never assigned,
// then compared with its zero value.
package zerovalue

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// CATCH: s is "" at the check, so the false side is impossible.
func NeverAssigned(ctx workflow.Context) error {
	var s string
	_ = act(ctx)
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: s is assigned from an input in between.
func AssignedFromInput(ctx workflow.Context, name string) error {
	var s string
	if name != "" {
		s = name
	}
	if s == "" {
		_ = act(ctx)
	}
	return nil
}
