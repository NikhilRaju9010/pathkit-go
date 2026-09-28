package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Selector shapes PathKit can't map safely: each workflow here is skipped
// with "workflow.Selector at <file>:<line> is not supported: <reason>".
// Every workflow in this file must stay unsupported (a CLI test analyzes
// the whole file and expects every one to be skipped).

func SelectorFromParam(ctx workflow.Context, sel workflow.Selector) (string, error) {
	sel.AddDefault(func() {})
	sel.Select(ctx)
	return "ok", nil
}

func newSel(ctx workflow.Context) workflow.Selector { return workflow.NewSelector(ctx) }

func SelectorFromHelper(ctx workflow.Context) (string, error) {
	sel := newSel(ctx)
	sel.AddDefault(func() {})
	sel.Select(ctx)
	return "ok", nil
}

func SelectorCreatedTwice(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel = workflow.NewSelector(ctx)
	sel.AddDefault(func() {})
	sel.Select(ctx)
	return "ok", nil
}

func SelectorAddInIf(ctx workflow.Context, x int) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {})
	if x > 0 {
		sel.AddDefault(func() {})
	}
	sel.Select(ctx)
	return "ok", nil
}

func SelectorAddInClosure(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddDefault(func() {})
	workflow.Go(ctx, func(ctx workflow.Context) {
		sel.AddReceive(workflow.GetSignalChannel(ctx, "s"), func(c workflow.ReceiveChannel, more bool) {})
	})
	sel.Select(ctx)
	return "ok", nil
}

func onTimeout(f workflow.Future) {}

func SelectorNamedCallback(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), onTimeout)
	sel.Select(ctx)
	return "ok", nil
}

func SelectorTwoSelects(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddDefault(func() {})
	sel.Select(ctx)
	sel.Select(ctx)
	return "ok", nil
}

func register(sel workflow.Selector) {}

func SelectorPassedToHelper(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddDefault(func() {})
	register(sel)
	sel.Select(ctx)
	return "ok", nil
}

func SelectorAddAfterSelect(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {})
	sel.Select(ctx)
	sel.AddDefault(func() {})
	return "ok", nil
}

func SelectorNoAdds(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.Select(ctx)
	return "ok", nil
}

func SelectorNotInVariable(ctx workflow.Context) (string, error) {
	workflow.NewSelector(ctx).AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {}).Select(ctx)
	return "ok", nil
}
