// Package unsafefn is safety fixture: a function that uses the unsafe
// package switches the engine off. Must stay testable.
package unsafefn

import "unsafe"

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func normalise(s string) string { return s + "" }

func UsesUnsafe(ctx workflow.Context) error {
	var s string
	_ = unsafe.Sizeof(s)
	if s == "" {
		_ = act(ctx)
	}
	return nil
}
