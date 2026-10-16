// Package pointerfield is safety fixture: a field behind a pointer is shared
// with whoever else holds the pointer, so nothing is kept about it across a
// call that gets the pointer. Must stay testable.
package pointerfield

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func normalise(s string) string { return s + "" }

type Order struct{ Item string }

func fill(o *Order) { o.Item = "filled" }

func FieldThroughPointer(ctx workflow.Context, o *Order) error {
	if o.Item == "" {
		_ = act(ctx)
	}
	fill(o)
	if o.Item == "" {
		_ = act(ctx)
	}
	return nil
}

func AliasOfPointer(ctx workflow.Context, o *Order) error {
	alias := o
	if o.Item == "" {
		return nil
	}
	fill(alias)
	if o.Item != "filled" {
		_ = act(ctx)
	}
	return nil
}
