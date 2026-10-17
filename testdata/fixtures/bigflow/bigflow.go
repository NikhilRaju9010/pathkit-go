// Package bigflow has workflows far over the path cap, to test exact counts,
// stages, pruning and facts across stages.
package bigflow

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func step(ctx workflow.Context) { _ = workflow.Sleep(ctx, time.Second) }

// TwentyFourIfs has 24 independent decisions in a row: 2^24 paths.
func TwentyFourIfs(ctx workflow.Context, in [24]bool) error {
	if in[0] {
		step(ctx)
	}
	if in[1] {
		step(ctx)
	}
	if in[2] {
		step(ctx)
	}
	if in[3] {
		step(ctx)
	}
	if in[4] {
		step(ctx)
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	if in[12] {
		step(ctx)
	}
	if in[13] {
		step(ctx)
	}
	if in[14] {
		step(ctx)
	}
	if in[15] {
		step(ctx)
	}
	if in[16] {
		step(ctx)
	}
	if in[17] {
		step(ctx)
	}
	if in[18] {
		step(ctx)
	}
	if in[19] {
		step(ctx)
	}
	if in[20] {
		step(ctx)
	}
	if in[21] {
		step(ctx)
	}
	if in[22] {
		step(ctx)
	}
	if in[23] {
		step(ctx)
	}
	return nil
}

// EarlyExits returns early at the first three checks, then has twenty
// decisions in a row: the early ends are paths of the first stage.
func EarlyExits(ctx workflow.Context, a, b, c bool, in [20]bool) error {
	if a {
		return nil
	}
	if b {
		return nil
	}
	if c {
		return nil
	}
	if in[0] {
		step(ctx)
	}
	if in[1] {
		step(ctx)
	}
	if in[2] {
		step(ctx)
	}
	if in[3] {
		step(ctx)
	}
	if in[4] {
		step(ctx)
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	if in[12] {
		step(ctx)
	}
	if in[13] {
		step(ctx)
	}
	if in[14] {
		step(ctx)
	}
	if in[15] {
		step(ctx)
	}
	if in[16] {
		step(ctx)
	}
	if in[17] {
		step(ctx)
	}
	if in[18] {
		step(ctx)
	}
	if in[19] {
		step(ctx)
	}
	return nil
}

// PrunedFlow: "disabled" never changes, so the true side of its check cannot be
// taken, and everything below it (4,194,304 paths) is impossible. The rest has
// 2,048 paths, more than the cap lists.
func PrunedFlow(ctx workflow.Context, a, b [11]bool) error {
	disabled := false
	if disabled {
		if a[0] {
			step(ctx)
		}
		if a[1] {
			step(ctx)
		}
		if a[2] {
			step(ctx)
		}
		if a[3] {
			step(ctx)
		}
		if a[4] {
			step(ctx)
		}
		if a[5] {
			step(ctx)
		}
		if a[6] {
			step(ctx)
		}
		if a[7] {
			step(ctx)
		}
		if a[8] {
			step(ctx)
		}
		if a[9] {
			step(ctx)
		}
		if a[10] {
			step(ctx)
		}
	}
	if b[0] {
		step(ctx)
	}
	if b[1] {
		step(ctx)
	}
	if b[2] {
		step(ctx)
	}
	if b[3] {
		step(ctx)
	}
	if b[4] {
		step(ctx)
	}
	if b[5] {
		step(ctx)
	}
	if b[6] {
		step(ctx)
	}
	if b[7] {
		step(ctx)
	}
	if b[8] {
		step(ctx)
	}
	if b[9] {
		step(ctx)
	}
	if b[10] {
		step(ctx)
	}
	return nil
}

// ContextsAllImpossible: "mode" is "a" on every way into the stage that asks
// whether it is "b", so that answer is impossible in every incoming state.
func ContextsAllImpossible(ctx workflow.Context, in [16]bool) error {
	mode := "a"
	if in[0] {
		step(ctx)
	}
	if in[1] {
		step(ctx)
	}
	if in[2] {
		step(ctx)
	}
	if in[3] {
		step(ctx)
	}
	if in[4] {
		step(ctx)
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if mode == "b" {
		step(ctx)
	}
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	if in[12] {
		step(ctx)
	}
	if in[13] {
		step(ctx)
	}
	if in[14] {
		step(ctx)
	}
	return nil
}

// ContextsDiffer: the first decisions set "v" differently on different ways in,
// so the later check "v == x" is possible in one of the incoming states, and
// "v == y" in none.
func ContextsDiffer(ctx workflow.Context, in [16]bool) error {
	v := ""
	if in[0] {
		v = "x"
	}
	if in[1] {
		step(ctx)
	}
	if in[2] {
		step(ctx)
	}
	if in[3] {
		step(ctx)
	}
	if in[4] {
		step(ctx)
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if v == "x" {
		step(ctx)
	}
	if v == "y" {
		step(ctx)
	}
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	if in[12] {
		step(ctx)
	}
	if in[13] {
		step(ctx)
	}
	if in[14] {
		step(ctx)
	}
	return nil
}

// ContextsCutOff: five values are set or not by the first decisions, so the
// stage that follows can start in 32 different states. With a limit of 16 the
// answer for "a1 == z" is only pending; with 64 it is impossible.
func ContextsCutOff(ctx workflow.Context, in [20]bool) error {
	var a1, a2, a3, a4, a5 string
	if in[0] {
		a1 = "x"
	}
	if in[1] {
		a2 = "x"
	}
	if in[2] {
		a3 = "x"
	}
	if in[3] {
		a4 = "x"
	}
	if in[4] {
		a5 = "x"
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if a1 == "z" {
		step(ctx)
	}
	_, _, _, _, _ = a1, a2, a3, a4, a5
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	return nil
}

// ContextsCutOffLater: the states are cut at 16 after the first decisions,
// and a decision comes two stages later. The cut must still count there: the
// states left out may be the ones where a1 is empty.
func ContextsCutOffLater(ctx workflow.Context, in [24]bool) error {
	var a1, a2, a3, a4, a5 string
	if in[0] {
		a1 = "x"
	}
	if in[1] {
		a2 = "x"
	}
	if in[2] {
		a3 = "x"
	}
	if in[3] {
		a4 = "x"
	}
	if in[4] {
		a5 = "x"
	}
	if in[5] {
		step(ctx)
	}
	if in[6] {
		step(ctx)
	}
	if in[7] {
		step(ctx)
	}
	if in[8] {
		step(ctx)
	}
	if in[9] {
		step(ctx)
	}
	if in[10] {
		step(ctx)
	}
	if in[11] {
		step(ctx)
	}
	if in[12] {
		step(ctx)
	}
	if in[13] {
		step(ctx)
	}
	if in[14] {
		step(ctx)
	}
	if in[15] {
		step(ctx)
	}
	if in[16] {
		step(ctx)
	}
	if in[17] {
		step(ctx)
	}
	if in[18] {
		step(ctx)
	}
	if in[19] {
		step(ctx)
	}
	if in[20] {
		step(ctx)
	}
	if a1 == "" {
		step(ctx)
	}
	_, _, _, _, _ = a1, a2, a3, a4, a5
	return nil
}

// CutThenReturn: with one state followed, the stage after the first decision
// is cut; the next stage has an early return, so only one state goes on. The
// cut must still count at the last decision.
func CutThenReturn(ctx workflow.Context, a bool, x bool) error {
	sv := "b"
	if a {
		sv = "a"
	}
	if x {
		return nil
	}
	if sv == "b" {
		step(ctx)
	}
	return nil
}
