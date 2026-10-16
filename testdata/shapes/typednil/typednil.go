// Package typednil is shape (c): an interface that holds a typed pointer is
// never a nil interface, even when the pointer itself is nil.
package typednil

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// CATCH: v holds a *string, so "v == nil" is never true.
func TypedNilInInterface(ctx workflow.Context, note *string) error {
	var v interface{} = note
	if v == nil {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: v is only set when the flag is true, so it can still be a nil
// interface.
func MaybeUnset(ctx workflow.Context, note *string, flag bool) error {
	var v interface{}
	if flag {
		v = note
	}
	if v == nil {
		_ = act(ctx)
	}
	return nil
}
