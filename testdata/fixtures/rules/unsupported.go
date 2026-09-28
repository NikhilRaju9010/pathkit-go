package rules

import (
	"go.temporal.io/sdk/workflow"
)

// Every workflow here uses a construct PathKit never maps (goto, Go's
// select), so it must be skipped with a clear message instead of drawn
// with a half-right map.

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
