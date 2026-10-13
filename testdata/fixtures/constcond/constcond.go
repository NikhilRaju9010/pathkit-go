// Package constcond holds analysis fixtures for the constant-condition
// detector: conditions the compiler knows the value of, and
// near-misses that only look constant.
package constcond

import (
	"math"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"go.temporal.io/sdk/workflow"
)

const debug = false
const (
	a = 3
	b = a * 2
)
const (
	first = iota
	second
)
const mode = "fast"

// A package-level var is NOT a constant, even if nothing assigns it: a test,
// another file or -ldflags -X can.
var enabled = false

// Depends on the machine.
const isLinux = runtime.GOOS == "linux"

func step(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// ---- catch ----

func CatchIfFalse(ctx workflow.Context) error {
	if false {
		return step(ctx)
	}
	return nil
}

func CatchNotTrue(ctx workflow.Context) error {
	if !true {
		return step(ctx)
	}
	return nil
}

func CatchArithmetic(ctx workflow.Context) error {
	if 1 > 2 {
		return step(ctx)
	}
	return nil
}

func CatchConstName(ctx workflow.Context) error {
	if debug {
		return step(ctx)
	}
	return nil
}

func CatchConstNegated(ctx workflow.Context) error {
	if !debug {
		return nil
	}
	return step(ctx)
}

func CatchConstExpression(ctx workflow.Context) error {
	if b > 5 {
		return nil
	}
	return step(ctx)
}

func CatchLenOfArray(ctx workflow.Context) error {
	if len([3]int{}) == 0 {
		return step(ctx)
	}
	return nil
}

func CatchIota(ctx workflow.Context) error {
	if second == 1 {
		return nil
	}
	return step(ctx)
}

func CatchStringConst(ctx workflow.Context) error {
	if mode == "slow" {
		return step(ctx)
	}
	return nil
}

// ---- near-miss: stays testable ----

func PackageVar(ctx workflow.Context) error {
	if enabled {
		return step(ctx)
	}
	return nil
}

func LocalVar(ctx workflow.Context) error {
	x := false
	if x {
		return step(ctx)
	}
	return nil
}

func RuntimeGOOS(ctx workflow.Context) error {
	if runtime.GOOS == "linux" {
		return step(ctx)
	}
	return nil
}

func IntSize(ctx workflow.Context) error {
	if strconv.IntSize == 64 {
		return step(ctx)
	}
	return nil
}

func MaxInt(ctx workflow.Context) error {
	if math.MaxInt == math.MaxInt64 {
		return step(ctx)
	}
	return nil
}

func ShiftExpression(ctx workflow.Context) error {
	if 1<<40 > 0 {
		return step(ctx)
	}
	return nil
}

func XorExpression(ctx workflow.Context) error {
	if ^uint(0) > 0 {
		return step(ctx)
	}
	return nil
}

func PlatformConst(ctx workflow.Context) error {
	if isLinux {
		return step(ctx)
	}
	return nil
}

func TaggedConst(ctx workflow.Context) error {
	if taggedOff {
		return step(ctx)
	}
	return nil
}

func MixedWithVariable(ctx workflow.Context, n int) error {
	if debug || n > 0 {
		return step(ctx)
	}
	return nil
}

func ConstantInSwitchTag(ctx workflow.Context) error {
	switch debug {
	case true:
		return step(ctx)
	}
	return nil
}

func ConstantLoopCondition(ctx workflow.Context) error {
	for debug {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

// A type of this package whose size depends on the machine.
type machineSized [unsafe.Sizeof(uintptr(0))]byte

func ArrayLengthThroughType(ctx workflow.Context) error {
	if len(machineSized{}) == 8 {
		return step(ctx)
	}
	return nil
}
