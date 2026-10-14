// Package historysuggest holds analysis fixtures for the history-limit
// detector.
package historysuggest

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func step(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// ---- catch: SDK suggests Continue-As-New ----

// suggested = true
func CatchDirect(ctx workflow.Context) error {
	if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
		return workflow.NewContinueAsNewError(ctx, CatchDirect)
	}
	return step(ctx)
}

// suggested = true, through a variable
func CatchViaInfo(ctx workflow.Context) error {
	info := workflow.GetInfo(ctx)
	if info.GetContinueAsNewSuggested() {
		return workflow.NewContinueAsNewError(ctx, CatchViaInfo)
	}
	return step(ctx)
}

// suggested = true, through a bool variable
func CatchViaBool(ctx workflow.Context) error {
	suggested := workflow.GetInfo(ctx).GetContinueAsNewSuggested()
	if suggested {
		return workflow.NewContinueAsNewError(ctx, CatchViaBool)
	}
	return step(ctx)
}

// suggested = false (negated)
func CatchNegated(ctx workflow.Context) error {
	if !workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
		return step(ctx)
	}
	return workflow.NewContinueAsNewError(ctx, CatchNegated)
}

// ---- weaker: a number compared with the history length/size ----

// large = true
func WeakLength(ctx workflow.Context) error {
	if workflow.GetInfo(ctx).GetCurrentHistoryLength() > 40000 {
		return workflow.NewContinueAsNewError(ctx, WeakLength)
	}
	return step(ctx)
}

// large = false (reversed)
func WeakSizeReversed(ctx workflow.Context) error {
	size := workflow.GetInfo(ctx).GetCurrentHistorySize()
	if size < 1000000 {
		return step(ctx)
	}
	return workflow.NewContinueAsNewError(ctx, WeakSizeReversed)
}

// ---- near-miss: nothing recorded ----

type notSDK struct{}

func (notSDK) GetContinueAsNewSuggested() bool { return true }

// A method of the same name on another type.
func OtherType(ctx workflow.Context) error {
	var s notSDK
	if s.GetContinueAsNewSuggested() {
		return nil
	}
	return step(ctx)
}

// A plain bool that happens to be called suggested.
func PlainBool(ctx workflow.Context, suggested bool) error {
	if suggested {
		return nil
	}
	return step(ctx)
}

// A compound condition.
func Compound(ctx workflow.Context, n int) error {
	if workflow.GetInfo(ctx).GetContinueAsNewSuggested() && n > 0 {
		return nil
	}
	return step(ctx)
}

// A loop condition is not read.
func LoopCondition(ctx workflow.Context) error {
	for !workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

// The history length compared with a variable, not a constant.
func LengthVsVariable(ctx workflow.Context, limit int) error {
	if workflow.GetInfo(ctx).GetCurrentHistoryLength() > limit {
		return nil
	}
	return step(ctx)
}

// A suggestion variable assigned twice.
func Reassigned(ctx workflow.Context) error {
	s := workflow.GetInfo(ctx).GetContinueAsNewSuggested()
	s = false
	if s {
		return nil
	}
	return step(ctx)
}
