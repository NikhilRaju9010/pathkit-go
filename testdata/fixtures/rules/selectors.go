package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// A workflow.Selector's Select call is a junction (CLAUDE.md D3): one
// exit per Add… call, labelled by what it waits for. Each exit's road is
// its callback's body, then the code after Select.

const ApproveSignal = "approve"

func UsesSelector(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Minute), func(f workflow.Future) {})
	sel.Select(ctx)
	return "ok", nil
}

func SignalOrTimer(ctx workflow.Context) (string, error) {
	out := ""
	timer := workflow.NewTimer(ctx, time.Hour)
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "go"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &out)
	})
	sel.AddFuture(timer, func(f workflow.Future) {
		out = "late"
	})
	sel.Select(ctx)
	return out, nil
}

// Every kind of exit label.
func SelectorAllKinds(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	lctx := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{StartToCloseTimeout: time.Minute})
	charge := workflow.ExecuteActivity(ctx, Charge, 1)
	child := workflow.ExecuteChildWorkflow(ctx, ChildFlow)
	local := workflow.ExecuteLocalActivity(lctx, Notify)
	approvals := workflow.GetSignalChannel(ctx, ApproveSignal)
	ch := workflow.NewChannel(ctx)
	out := workflow.NewChannel(ctx)
	sel := workflow.NewNamedSelector(ctx, "everything")
	sel.AddReceive(approvals, func(c workflow.ReceiveChannel, more bool) {})
	sel.AddFuture(charge, func(f workflow.Future) {})
	sel.AddFuture(child, func(f workflow.Future) {})
	sel.AddFuture(local, func(f workflow.Future) {})
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {})
	sel.AddSend(out, 1, func() {})
	sel.AddDefault(func() {})
	sel.Select(ctx)
	return "ok", nil
}

// A return inside a callback returns from the callback: the road goes on
// after Select.
func CallbackBranches(ctx workflow.Context, x int) (string, error) {
	out := ""
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "s"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &out)
		if out == "" {
			out = "empty"
			return
		}
		out += "!"
	})
	sel.AddDefault(func() { out = "none" })
	sel.Select(ctx)
	if x > 0 {
		return out, nil
	}
	return "", nil
}

// An error check inside a callback, on an err set inside that callback.
func ErrCheckInCallback(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	v := ""
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.ExecuteActivity(ctx, Charge, 1), func(f workflow.Future) {
		err := f.Get(ctx, &v)
		if err != nil {
			v = "failed"
		}
	})
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) { v = "late" })
	sel.Select(ctx)
	return v, nil
}

// Select inside a loop, Add… calls before it: fine. The selector is
// numbered at its first Add… call, so it is J1 and the loop is J2.
func SelectInLoop(ctx workflow.Context, n int) (int, error) {
	got := 0
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "s"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, nil)
		got++
	})
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {})
	for i := 0; i < n; i++ {
		sel.Select(ctx)
	}
	return got, nil
}

// Chained Add… calls in one statement are fine; two timers get
// "timeout" and "timeout #2".
func ChainedAdds(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Minute), func(f workflow.Future) {}).
		AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {})
	sel.Select(ctx)
	return "ok", nil
}
