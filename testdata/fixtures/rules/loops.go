package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Loops (CLAUDE.md D3, amended 2026-09-28): a loop is a junction when it
// contains a Temporal call or any junction; otherwise it is transparent
// and walked once. A loop junction has exits iterate / exit and a retry
// edge, each used at most once per path.

// Transparent: no Temporal call, no junction.
func UsesFor(ctx workflow.Context, n int) (string, error) {
	for i := 0; i < n; i++ {
		_ = i
	}
	return "ok", nil
}

// Transparent too.
func UsesRange(ctx workflow.Context, items []string) (string, error) {
	for range items {
	}
	return "ok", nil
}

// Still transparent: the only if inside is a transparent error check (not
// after a Temporal call), so it is not a junction. Only the if after the
// loop counts.
func TransparentLoopBeforeIf(ctx workflow.Context, xs []int) (string, error) {
	total := 0
	for _, x := range xs {
		total += x
		if _, err := decode("x"); err != nil {
			continue
		}
	}
	if total > 100 {
		return "big", nil
	}
	return "small", nil
}

// Still transparent: the if inside is ignored by pragma.
func IgnoredIfInLoop(ctx workflow.Context, n int) (string, error) {
	for i := 0; i < n; i++ {
		//pathkit:ignore
		if i > 5 {
			break
		}
	}
	return "ok", nil
}

func LoopWithActivity(ctx workflow.Context, n int) (string, error) {
	ctx = withOptions(ctx)
	for i := 0; i < n; i++ {
		if err := workflow.ExecuteActivity(ctx, Charge, i).Get(ctx, nil); err != nil {
			return "", err
		}
	}
	return "ok", nil
}

// No Temporal call, but a junction inside: a loop junction under the
// amended rule, so repeated trips can be folded.
func LoopWithOnlyIf(ctx workflow.Context, xs []int) (int, error) {
	big := 0
	for _, x := range xs {
		if x > 10 {
			big++
		}
	}
	return big, nil
}

// "for {}" has no exit: it leaves only by break or return.
func ForeverWithBreak(ctx workflow.Context, limit int) (int, error) {
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

// continue goes round again: a retry.
func ContinueInLoop(ctx workflow.Context, n int) (string, error) {
	ctx = withOptions(ctx)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			continue
		}
		if err := workflow.ExecuteActivity(ctx, Charge, i).Get(ctx, nil); err != nil {
			return "", err
		}
	}
	return "ok", nil
}

func NestedLoops(ctx workflow.Context, outer, inner int) error {
	ctx = withOptions(ctx)
	for i := 0; i < outer; i++ {
		for j := 0; j < inner; j++ {
			if err := workflow.ExecuteActivity(ctx, Charge, i*10+j).Get(ctx, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// continue outer is the outer loop's retry; break outer leaves both loops
// without an exit step.
func LabeledBreakContinue(ctx workflow.Context, grid [][]int) error {
	ctx = withOptions(ctx)
outer:
	for _, row := range grid {
		for _, x := range row {
			if x < 0 {
				continue outer
			}
			if x == 0 {
				break outer
			}
			if err := workflow.ExecuteActivity(ctx, Charge, x).Get(ctx, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
