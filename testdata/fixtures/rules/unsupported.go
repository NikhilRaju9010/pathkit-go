package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Every workflow here uses a construct PathKit can't map (yet, or ever:
// goto and Go's select), so it must be skipped with a clear message
// instead of drawn with a half-right map.

func UsesFor(ctx workflow.Context, n int) (string, error) {
	for i := 0; i < n; i++ {
		_ = i
	}
	return "ok", nil
}

func UsesRange(ctx workflow.Context, items []string) (string, error) {
	for range items {
	}
	return "ok", nil
}

func UsesGoSelect(ctx workflow.Context, ch chan int) (string, error) {
	select {
	case <-ch:
	default:
	}
	return "ok", nil
}

func UsesLabel(ctx workflow.Context, x int) (string, error) {
	if x > 0 {
		goto done
	}
done:
	return "ok", nil
}

func UsesSelector(ctx workflow.Context) (string, error) {
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Minute), func(f workflow.Future) {})
	sel.Select(ctx)
	return "ok", nil
}

func UsesAwaitResult(ctx workflow.Context) (string, error) {
	done := false
	ok, err := workflow.AwaitWithTimeout(ctx, time.Hour, func() bool { return done })
	if err != nil {
		return "", err
	}
	if !ok {
		return "timeout", nil
	}
	return "done", nil
}

func UsesReceiveWithTimeout(ctx workflow.Context) (string, error) {
	var v string
	if ok, _ := workflow.GetSignalChannel(ctx, "s").ReceiveWithTimeout(ctx, time.Minute, &v); ok {
		return v, nil
	}
	return "none", nil
}

func UsesDeferCompensation(ctx workflow.Context) (err error) {
	ctx = withOptions(ctx)
	defer func() {
		if err != nil {
			_ = workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
		}
	}()
	return nil
}

// A defer that makes no Temporal call is fine in M2.
func PlainDefer(ctx workflow.Context) (string, error) {
	cleanup := func() {}
	defer cleanup()
	return "ok", nil
}
