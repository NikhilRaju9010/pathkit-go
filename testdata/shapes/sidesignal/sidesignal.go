// Package sidesignal is safety fixture (m): values filled in by
// SideEffect(...).Get(&v) and by a signal Receive(ctx, &v). Must stay
// testable.
package sidesignal

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func SideEffectValue(ctx workflow.Context) error {
	var v string
	_ = workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} { return "id-1" }).Get(&v)
	if v == "" {
		_ = act(ctx)
	}
	return nil
}

func SignalValue(ctx workflow.Context) error {
	var s string
	workflow.GetSignalChannel(ctx, "note").Receive(ctx, &s)
	if s == "" {
		_ = act(ctx)
	}
	return nil
}
