// Package valueflow is shape (a): a comma-ok type assertion, a copy under
// "if ok", then a check of the copy. Made-up names; no real project.
package valueflow

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// CATCH: when ok is false, id stays "", so "id == \"\"" must be true. The
// path (ok false, id not empty) is impossible, but the link goes through
// an assignment, so two different variables look unrelated.
func ContradictionThroughAssign(ctx workflow.Context, order map[string]interface{}) error {
	id := ""
	v, ok := order["id"].(string)
	if ok {
		id = v
	}
	if id == "" {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: an unknown call changes id after the copy, so nothing is known
// about it any more. Every path stays testable.
func ReassignedByCall(ctx workflow.Context, order map[string]interface{}) error {
	id := ""
	v, ok := order["id"].(string)
	if ok {
		id = v
	}
	id = normalise(id)
	if id == "" {
		_ = act(ctx)
	}
	return nil
}

func normalise(s string) string { return s }
