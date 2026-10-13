// Package dup exists twice, in a/dup and b/dup, with the same workflow name
// but different code. PathKit must never mix the two up.
package dup

import "go.temporal.io/sdk/workflow"

// Work in a/dup has two paths: positive or not.
func Work(ctx workflow.Context, n int) (string, error) {
	if n > 0 {
		return "a-positive", nil
	}
	return "a-not-positive", nil
}
