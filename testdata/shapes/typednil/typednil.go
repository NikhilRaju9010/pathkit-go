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

// v is only set when the flag is true, so with the flag false it is still a
// nil interface (v == nil cannot be false then), and with the flag true it
// holds a *string, which is never a nil interface.
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

// NEAR-MISS: an unknown call changes v after the flag is read, so nothing is
// known about it at the check.
func MaybeUnsetThenCall(ctx workflow.Context, note *string, flag bool) error {
	var v interface{}
	if flag {
		v = note
	}
	v = pick(v)
	if v == nil {
		_ = act(ctx)
	}
	return nil
}

func pick(v interface{}) interface{} { return v }
