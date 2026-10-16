// Package ptrreceiver is safety fixture (k): a method with a pointer
// receiver changes the struct (Go takes its address without writing &).
// Must stay testable.
package ptrreceiver

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

type tally struct{ name string }

func (t *tally) fill() { t.name = "filled" }

func PointerReceiverCall(ctx workflow.Context) error {
	var t tally
	t.fill()
	if t.name == "" {
		_ = act(ctx)
	}
	return nil
}
