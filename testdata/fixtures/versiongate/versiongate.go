// Package versiongate holds analysis fixtures for the version-gate-old-branch
// detector.
package versiongate

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func step(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// ---- catch: the exit named in the comment is the OLD side ----

// old = true
func CatchEqDefault(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-a", workflow.DefaultVersion, 1)
	if v == workflow.DefaultVersion {
		return nil
	}
	return step(ctx)
}

// old = false
func CatchNeDefault(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-b", workflow.DefaultVersion, 1)
	if v != workflow.DefaultVersion {
		return step(ctx)
	}
	return nil
}

// old = true
func CatchLess(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-c", workflow.DefaultVersion, 2)
	if v < 2 {
		return nil
	}
	return step(ctx)
}

// old = false
func CatchGreaterEq(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-d", workflow.DefaultVersion, 1)
	if v >= 1 {
		return step(ctx)
	}
	return nil
}

// old = true (number on the left)
func CatchFlipped(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-e", workflow.DefaultVersion, 1)
	if 1 > v {
		return nil
	}
	return step(ctx)
}

// old = true (the call is inside the condition)
func CatchInline(ctx workflow.Context) error {
	if workflow.GetVersion(ctx, "change-f", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return nil
	}
	return step(ctx)
}

// old = the case for DefaultVersion
func CatchSwitch(ctx workflow.Context) error {
	switch workflow.GetVersion(ctx, "change-g", workflow.DefaultVersion, 1) {
	case workflow.DefaultVersion:
		return nil
	default:
		return step(ctx)
	}
}

// old = both the DefaultVersion case and the case for version 1 (the
// latest is 2)
func CatchSwitchTwoOld(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-h", workflow.DefaultVersion, 2)
	switch v {
	case workflow.DefaultVersion:
		return nil
	case 1:
		return step(ctx)
	}
	return nil
}

// ---- near-miss: nothing recorded ----

func helperTest(v workflow.Version) bool { return v == workflow.DefaultVersion }

// The version is tested inside a helper.
func PassedToHelper(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-i", workflow.DefaultVersion, 1)
	if helperTest(v) {
		return nil
	}
	return step(ctx)
}

// Compared with something that is not a constant.
func NotConstant(ctx workflow.Context, want workflow.Version) error {
	v := workflow.GetVersion(ctx, "change-j", workflow.DefaultVersion, 1)
	if v == want {
		return nil
	}
	return step(ctx)
}

// A variable that only has "version" in its name.
func JustAName(ctx workflow.Context, version int) error {
	if version == -1 {
		return nil
	}
	return step(ctx)
}

// min and max are not constants, so the two sides can't be checked.
func RangeNotConstant(ctx workflow.Context, max workflow.Version) error {
	v := workflow.GetVersion(ctx, "change-k", workflow.DefaultVersion, max)
	if v == workflow.DefaultVersion {
		return nil
	}
	return step(ctx)
}

// The check only has one possible side: version 1 is both min and max.
func OneSidedRange(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-l", 1, 1)
	if v >= 1 {
		return nil
	}
	return step(ctx)
}

// == with a middle version is ambiguous (not the oldest).
func MiddleVersion(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-m", workflow.DefaultVersion, 2)
	if v == 1 {
		return nil
	}
	return step(ctx)
}

// The version variable is assigned twice.
func Reassigned(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-n", workflow.DefaultVersion, 1)
	v = 1
	if v == workflow.DefaultVersion {
		return nil
	}
	return step(ctx)
}

// A compound condition is not read.
func Compound(ctx workflow.Context, n int) error {
	v := workflow.GetVersion(ctx, "change-o", workflow.DefaultVersion, 1)
	if v == workflow.DefaultVersion && n > 0 {
		return nil
	}
	return step(ctx)
}

// A switch on the version whose case is the LATEST version only.
func SwitchLatestOnly(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-p", workflow.DefaultVersion, 1)
	switch v {
	case 1:
		return step(ctx)
	}
	return nil
}

type notSDK struct{}

func (notSDK) GetVersion(ctx workflow.Context, id string, min, max int) int { return max }

// A method called GetVersion that is not the SDK's.
func NotTheSDK(ctx workflow.Context) error {
	var s notSDK
	v := s.GetVersion(ctx, "change-q", -1, 1)
	if v == -1 {
		return nil
	}
	return step(ctx)
}

// The version variable is changed with ++.
func Incremented(ctx workflow.Context) error {
	v := workflow.GetVersion(ctx, "change-r", workflow.DefaultVersion, 1)
	v++
	if v == workflow.DefaultVersion {
		return nil
	}
	return step(ctx)
}

// old = true: the gate is kept in a bool variable that is set once.
func CatchBoolVar(ctx workflow.Context) error {
	newBehaviour := workflow.GetVersion(ctx, "change-s", workflow.DefaultVersion, 1) != workflow.DefaultVersion
	if newBehaviour {
		return step(ctx)
	}
	return nil
}

// old = false: the same variable, tested with !.
func CatchBoolVarNegated(ctx workflow.Context) error {
	newBehaviour := workflow.GetVersion(ctx, "change-t", workflow.DefaultVersion, 1) != workflow.DefaultVersion
	if !newBehaviour {
		return nil
	}
	return step(ctx)
}

func useNewRules(ctx workflow.Context) bool {
	return workflow.GetVersion(ctx, "change-u", workflow.DefaultVersion, 1) != workflow.DefaultVersion
}

// old = false: the gate is a small function of the package.
func CatchHelperGate(ctx workflow.Context) error {
	if useNewRules(ctx) {
		return step(ctx)
	}
	return nil
}

func isBig(n int) bool {
	return n > 10
}

// A function that is not a version gate is not read as one.
func NotAGateFunction(ctx workflow.Context, n int) error {
	if isBig(n) {
		return step(ctx)
	}
	return nil
}

func gateWithTwoStatements(ctx workflow.Context) bool {
	_ = ctx
	return workflow.GetVersion(ctx, "change-v", workflow.DefaultVersion, 1) != workflow.DefaultVersion
}

// A gate function with more than the return is not read.
func GateFunctionWithMore(ctx workflow.Context) error {
	if gateWithTwoStatements(ctx) {
		return step(ctx)
	}
	return nil
}
