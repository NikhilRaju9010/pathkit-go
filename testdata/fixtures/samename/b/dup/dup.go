// Package dup exists twice, in a/dup and b/dup, with the same workflow name
// but different code. PathKit must never mix the two up.
package dup

import "go.temporal.io/sdk/workflow"

// Work in b/dup has three paths: positive, negative or zero.
func Work(ctx workflow.Context, n int) (string, error) {
	if n > 0 {
		return "b-positive", nil
	}
	if n < 0 {
		return "b-negative", nil
	}
	return "b-zero", nil
}
