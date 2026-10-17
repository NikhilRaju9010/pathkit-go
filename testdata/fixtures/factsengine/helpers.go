package factsengine

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// ---- helpers: functions of the package that call the Temporal SDK are
// followed, one copy per call (docs/DESIGN.md D15) ----

// checkParam changes its own parameter: the value it leaves in n belongs to
// that call only.
func checkParam(ctx workflow.Context, n int) error {
	if n == 5 {
		return workflow.Sleep(ctx, time.Second)
	}
	n = 5
	_ = n
	return nil
}

// The second call starts with a new n, whatever the first call left in its n.
func PossibleHelperParamReused(ctx workflow.Context) error {
	if err := checkParam(ctx, 1); err != nil {
		return err
	}
	return checkParam(ctx, 5)
}

// mode is a constant inside the helper: the branch cannot be taken.
func constantInside(ctx workflow.Context) error {
	mode := "a"
	if mode == "b" {
		return workflow.Sleep(ctx, time.Second)
	}
	return workflow.Sleep(ctx, time.Minute)
}

func ImpossibleHelperLocal(ctx workflow.Context) error {
	return constantInside(ctx)
}

// A helper that is a closure writes the caller's variable.
func PossibleHelperClosureWrites(ctx workflow.Context) error {
	flag := false
	set := func(ctx workflow.Context) error {
		flag = true
		return workflow.Sleep(ctx, time.Second)
	}
	if err := set(ctx); err != nil {
		return err
	}
	if flag {
		return workflow.Sleep(ctx, time.Minute)
	}
	return nil
}

// A helper that gets the address of the caller's variable writes it.
func bump(ctx workflow.Context, n *int) error {
	*n = 7
	return workflow.Sleep(ctx, time.Second)
}

func PossibleHelperPointerArg(ctx workflow.Context) error {
	n := 0
	if err := bump(ctx, &n); err != nil {
		return err
	}
	if n == 0 {
		return workflow.Sleep(ctx, time.Minute)
	}
	return nil
}

// What the caller knows before the call still holds after it when the
// helper cannot reach it.
func ImpossibleKnownAcrossHelper(ctx workflow.Context, x string) error {
	if x == "a" {
		if err := checkParam(ctx, 1); err != nil {
			return err
		}
		if x == "b" {
			return workflow.Sleep(ctx, time.Minute)
		}
	}
	return nil
}
