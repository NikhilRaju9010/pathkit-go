// Package rules holds one tiny workflow per analyzer rule. PathKit's own
// tests analyze these; they are never run as workflows.
package rules

import (
	"context"
	"errors"
	"time"

	"go.temporal.io/sdk/workflow"
)

func Charge(ctx context.Context, amount int) (string, error) { return "ok", nil }

func Notify(ctx context.Context) error { return nil }

func ChildFlow(ctx workflow.Context) (string, error) { return "child", nil }

func decode(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	return len(s), nil
}

func withOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}
