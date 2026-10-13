// Package loopfirstnamed holds near-misses for "loop-exit-before-first-trip"
// whose variable has a named bool type. They are analysis-only: PathKit
// cannot record a loop condition of a named bool type (a known limit), so
// they are not run under "pathkit test".
package loopfirstnamed

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type In struct{ Flag bool }

// Flag is a named bool with a pointer-receiver method.
type Flag bool

func (f *Flag) Set() { *f = true }

// NEAR-MISS (same review): a pointer-receiver method takes &v by itself.
func MethodSets(ctx workflow.Context, in In) error {
	var v Flag
	if in.Flag {
		v.Set()
	}
	for !v {
		_ = workflow.Sleep(ctx, time.Second)
		v = true
	}
	return nil
}

// NEAR-MISS (same review): a method value.
func MethodValueSets(ctx workflow.Context, in In) error {
	var v Flag
	set := v.Set
	if in.Flag {
		set()
	}
	for !v {
		_ = workflow.Sleep(ctx, time.Second)
		v = true
	}
	return nil
}
