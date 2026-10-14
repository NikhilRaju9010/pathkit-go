// Package loopfirst holds the shapes of the detector
// "loop-exit-before-first-trip": a `for cond` loop whose condition is true
// the first time it is tested, so it always runs at least once.
package loopfirst

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type In struct {
	Flag  bool
	Items []string
}

func set(b *bool) { *b = true }

// CATCH: the flag starts false and is only set inside the loop.
func FlagLoop(ctx workflow.Context, in In) error {
	done := false
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// CATCH: zero value, positive condition on a flag that starts true.
func KeepGoing(ctx workflow.Context, in In) error {
	var stop bool
	keep := true
	for keep {
		_ = workflow.Sleep(ctx, time.Second)
		keep = false
	}
	_ = stop
	return nil
}

// CATCH: the variable is declared again on every trip of the outer loop.
func DeclaredInOuterLoop(ctx workflow.Context, in In) error {
	for range in.Items {
		done := false
		for !done {
			_ = workflow.Sleep(ctx, time.Second)
			done = true
		}
	}
	return nil
}

// NEAR-MISS: set before the loop on one branch.
func SetBefore(ctx workflow.Context, in In) error {
	done := false
	if in.Flag {
		done = true
	}
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: a closure that sets it is defined before the loop and called.
func ClosureSetsBefore(ctx workflow.Context, in In) error {
	done := false
	stop := func() { done = true }
	if in.Flag {
		stop()
	}
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: the address goes to a function before the loop.
func AddressBefore(ctx workflow.Context, in In) error {
	done := false
	if in.Flag {
		set(&done)
	}
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: a parameter has any value.
func FromParameter(ctx workflow.Context, done bool) error {
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: declared outside the outer loop, so a later trip sees the
// value the earlier trip left.
func DeclaredOutsideOuterLoop(ctx workflow.Context, in In) error {
	done := false
	for range in.Items {
		for !done {
			_ = workflow.Sleep(ctx, time.Second)
			done = true
		}
	}
	return nil
}

// NEAR-MISS: assigned from an input before the loop.
func AssignedBefore(ctx workflow.Context, in In) error {
	done := false
	done = in.Flag
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: the loop is never entered (the condition is false first), so
// only "iterate" is impossible: nothing is claimed.
func NeverEntered(ctx workflow.Context, in In) error {
	done := true
	for !done {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

// NEAR-MISS: a compound condition.
func Compound(ctx workflow.Context, in In) error {
	done := false
	for !done && in.Flag {
		_ = workflow.Sleep(ctx, time.Second)
		done = true
	}
	return nil
}

var stopped bool

// NEAR-MISS: a package-level variable can be changed by anything.
func PackageLevel(ctx workflow.Context, in In) error {
	for !stopped {
		_ = workflow.Sleep(ctx, time.Second)
		stopped = true
	}
	return nil
}

// NEAR-MISS (found by the independent review): an assignment target in
// parentheses.
func ParenAssign(ctx workflow.Context, in In) error {
	v := false
	if in.Flag {
		(v) = true
	}
	for !v {
		_ = workflow.Sleep(ctx, time.Second)
		v = true
	}
	return nil
}
