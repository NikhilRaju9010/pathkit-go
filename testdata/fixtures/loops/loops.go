// Package loops holds workflows with loops that PathKit's end-to-end test
// runs for real under "pathkit test", so that traces with several trips
// are folded by the loop rule (CLAUDE.md D3).
package loops

import (
	"context"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func withOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// Step does one unit of work; it fails for good when n == failAt.
func Step(ctx context.Context, n, failAt int) error {
	if n == failAt {
		return temporal.NewNonRetryableApplicationError("step failed", "StepFailed", nil)
	}
	return nil
}

// Check says whether x is what we look for (a multiple of 7). A negative
// x fails for good.
func Check(ctx context.Context, x int) (bool, error) {
	if x < 0 {
		return false, temporal.NewNonRetryableApplicationError("negative", "Negative", nil)
	}
	return x%7 == 0, nil
}

type Grid struct {
	Outer, Inner, FailAt int
}

// NestedWorkflow runs a step for every (i, j): an inner loop inside an
// outer loop.
func NestedWorkflow(ctx workflow.Context, g Grid) (int, error) {
	ctx = withOptions(ctx)
	done := 0
	for i := 0; i < g.Outer; i++ {
		for j := 0; j < g.Inner; j++ {
			if err := workflow.ExecuteActivity(ctx, Step, i*10+j, g.FailAt).Get(ctx, nil); err != nil {
				return done, err
			}
			done++
		}
	}
	return done, nil
}

// ScanWorkflow finds the first item that passes Check, skipping zeros: a
// range loop with continue and break.
func ScanWorkflow(ctx workflow.Context, items []int) (int, error) {
	ctx = withOptions(ctx)
	found := -1
	for i, x := range items {
		if x == 0 {
			continue
		}
		var ok bool
		if err := workflow.ExecuteActivity(ctx, Check, x).Get(ctx, &ok); err != nil {
			return -1, err
		}
		if ok {
			found = i
			break
		}
	}
	return found, nil
}

// WaitWorkflow sleeps an hour at a time until limit hours passed: a
// "for {}" left by break.
func WaitWorkflow(ctx workflow.Context, limit int) (int, error) {
	n := 0
	for {
		if err := workflow.Sleep(ctx, time.Hour); err != nil {
			return n, err
		}
		n++
		if n >= limit {
			break
		}
	}
	return n, nil
}

// GridWorkflow uses the numbers row by row: a negative number skips the
// rest of its row (continue outer), a zero stops everything (break outer).
func GridWorkflow(ctx workflow.Context, grid [][]int) (int, error) {
	ctx = withOptions(ctx)
	used := 0
outer:
	for _, row := range grid {
		for _, x := range row {
			if x < 0 {
				continue outer
			}
			if x == 0 {
				break outer
			}
			if err := workflow.ExecuteActivity(ctx, Check, x).Get(ctx, nil); err != nil {
				return used, err
			}
			used++
		}
	}
	return used, nil
}

// SumWorkflow's loop has no Temporal call and no junction: it is
// transparent and records nothing, however many items there are.
func SumWorkflow(ctx workflow.Context, xs []int) (string, error) {
	total := 0
	for _, x := range xs {
		total += x
	}
	if total > 100 {
		return "big", nil
	}
	return "small", nil
}

// CountBigWorkflow's loop has no Temporal call but has a junction (the
// if), so it is a loop junction (the rule as amended on 2026-09-28) and a
// run with several items folds onto one path.
func CountBigWorkflow(ctx workflow.Context, xs []int) (int, error) {
	big := 0
	for _, x := range xs {
		if x > 10 {
			big++
		}
	}
	return big, nil
}
