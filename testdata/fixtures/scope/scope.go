// Package scope holds functions for PathKit's scope-config tests
// (.pathkitrc.json, CLAUDE.md D8): some the automatic workflow rule finds,
// some only "include" can add, and one that can never be a workflow.
package scope

import (
	"context"

	"go.temporal.io/sdk/workflow"
)

// VisibleFlow is an ordinary workflow: the automatic rule finds it.
func VisibleFlow(ctx workflow.Context) error { return nil }

// lowerFlow is unexported, so the automatic rule misses it; "include"
// can add it (labelled "added by config"). It has a real test.
func lowerFlow(ctx workflow.Context, n int) (string, error) {
	if n > 0 {
		return "positive", nil
	}
	return "other", nil
}

// NoErrorFlow has no error result, so the automatic rule misses it too.
func NoErrorFlow(ctx workflow.Context) string { return "ok" }

// sendEmail takes a context.Context, not a workflow.Context: including it
// is an error (D8).
func sendEmail(ctx context.Context, to string) error { return nil }

// Svc has a method workflow, to test the "pkg.(*Type).Method" name form.
type Svc struct{}

func (s *Svc) Handle(ctx workflow.Context) error { return nil }

var _ = sendEmail
