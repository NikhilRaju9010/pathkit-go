// Package unnamedctx has workflows whose workflow.Context parameter is
// written as "_" (found on a public Temporal project): they have decisions
// but never touch the context, so the name is not needed in their own code.
package unnamedctx

import (
	"errors"

	"go.temporal.io/sdk/workflow"
)

// Sign says whether n is positive, negative or zero.
func Sign(_ workflow.Context, n int) (string, error) {
	if n > 0 {
		return "positive", nil
	}
	if n < 0 {
		return "negative", nil
	}
	return "zero", nil
}

// Strict fails on an empty name; the second parameter is also unnamed.
func Strict(_ workflow.Context, name string, _ int) error {
	if name == "" {
		return errors.New("empty name")
	}
	return nil
}
