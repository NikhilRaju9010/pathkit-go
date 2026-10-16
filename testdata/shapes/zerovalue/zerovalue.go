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

// CATCH, found by the fact engine: s is a copy of name when name is not
// empty, and still "" when it is, so the checks cannot disagree with the
// first decision.
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

// NEAR-MISS: s is assigned the result of a call PathKit cannot see into, so
// when name is not empty nothing is known about it.
func AssignedFromCall(ctx workflow.Context, name string) error {
	var s string
	if name != "" {
		s = normalise(name)
	}
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

func normalise(s string) string { return s }
