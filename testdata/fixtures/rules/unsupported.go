package rules

import (
	"go.temporal.io/sdk/workflow"
)

// Every workflow here uses a construct PathKit can't map (yet, or ever:
// goto and Go's select), so it must be skipped with a clear message
// instead of drawn with a half-right map.

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
