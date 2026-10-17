// Package clashhelper has a helper function that already uses a name the
// marked-up copy needs.
package clashhelper

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func sleepy(ctx workflow.Context) error {
	pathkitH := time.Second
	if err := workflow.Sleep(ctx, pathkitH); err != nil {
		return err
	}
	return nil
}

func Flow(ctx workflow.Context) error {
	if err := sleepy(ctx); err != nil {
		return err
	}
	return nil
}
