// Package waits holds workflows that race a signal against a timer
// (workflow.Selector) and wait with a timeout (AwaitWithTimeout,
// ReceiveWithTimeout, ReceiveAsync). PathKit's end-to-end test runs both
// sides of every race for real under "pathkit test".
package waits

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Lookup is a quick activity.
func Lookup(ctx context.Context, key string) (string, error) { return "value of " + key, nil }

// RaceWorkflow waits for an "answer" signal, or gives up when the timer
// fires first.
func RaceWorkflow(ctx workflow.Context, timeout time.Duration) (string, error) {
	answer := ""
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timer := workflow.NewTimer(timerCtx, timeout)
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "answer"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &answer)
		cancelTimer()
	})
	sel.AddFuture(timer, func(f workflow.Future) {
		answer = "no answer"
	})
	sel.Select(ctx)
	return answer, nil
}

// PendingWorkflow waits an hour, then takes a signal if one is waiting,
// or moves on (AddDefault).
func PendingWorkflow(ctx workflow.Context) (string, error) {
	_ = workflow.Sleep(ctx, time.Hour)
	got := ""
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "note"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &got)
	})
	sel.AddDefault(func() {
		got = "nothing"
	})
	sel.Select(ctx)
	return got, nil
}

// LookupWorkflow races an activity against a one-hour timer. The
// activity's error is checked inside its callback.
func LookupWorkflow(ctx workflow.Context, key string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	result := ""
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.ExecuteActivity(ctx, Lookup, key), func(f workflow.Future) {
		if err := f.Get(ctx, &result); err != nil {
			result = "lookup failed"
		}
	})
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {
		result = "too slow"
	})
	sel.Select(ctx)
	return result, nil
}

// CollectWorkflow checks for an "item" signal once an hour, rounds times:
// a Select inside a loop, so a run with several rounds is folded.
func CollectWorkflow(ctx workflow.Context, rounds int) (int, error) {
	got := 0
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "item"), func(c workflow.ReceiveChannel, more bool) {
		var item string
		c.Receive(ctx, &item)
		got++
	})
	sel.AddDefault(func() {})
	for i := 0; i < rounds; i++ {
		_ = workflow.Sleep(ctx, time.Hour)
		sel.Select(ctx)
	}
	return got, nil
}

// ApproveWorkflow waits up to timeout for an "approve" signal with
// AwaitWithTimeout.
func ApproveWorkflow(ctx workflow.Context, timeout time.Duration) (string, error) {
	approved := false
	workflow.Go(ctx, func(ctx workflow.Context) {
		workflow.GetSignalChannel(ctx, "approve").Receive(ctx, nil)
		approved = true
	})
	ok, err := workflow.AwaitWithTimeout(ctx, timeout, func() bool { return approved })
	if err != nil {
		return "", err
	}
	if !ok {
		return "expired", nil
	}
	return "approved", nil
}

// ReadWorkflow waits up to timeout for a "data" signal with
// ReceiveWithTimeout.
func ReadWorkflow(ctx workflow.Context, timeout time.Duration) (string, error) {
	var v string
	ok, _ := workflow.GetSignalChannel(ctx, "data").ReceiveWithTimeout(ctx, timeout, &v)
	if ok {
		return v, nil
	}
	return "none", nil
}

// PeekWorkflow waits an hour, then checks for a "data" signal without
// blocking (ReceiveAsync).
func PeekWorkflow(ctx workflow.Context) (string, error) {
	_ = workflow.Sleep(ctx, time.Hour)
	var v string
	if !workflow.GetSignalChannel(ctx, "data").ReceiveAsync(&v) {
		return "none", nil
	}
	return v, nil
}
