package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// A selector built and used in one chained expression: two exits.
func SelectorChained(ctx workflow.Context) (string, error) {
	got := ""
	workflow.NewSelector(ctx).
		AddReceive(workflow.GetSignalChannel(ctx, "go"), func(c workflow.ReceiveChannel, more bool) { got = "signal" }).
		AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) { got = "timer" }).
		Select(ctx)
	return got, nil
}

// A chain whose callback is not written inline cannot be followed.
func SelectorChainedNotInline(ctx workflow.Context) (string, error) {
	cb := func(f workflow.Future) {}
	workflow.NewSelector(ctx).AddFuture(workflow.NewTimer(ctx, time.Hour), cb).Select(ctx)
	return "ok", nil
}

// One Add… call, chained (it used to be refused as "kept in a variable").
func SelectorChainedOne(ctx workflow.Context) (string, error) {
	workflow.NewSelector(ctx).AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {}).Select(ctx)
	return "ok", nil
}
