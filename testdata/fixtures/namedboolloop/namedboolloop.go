// Package namedboolloop holds loop conditions that are not of the plain
// type bool, for the message "pathkit test" prints before it stops. They
// are analysis-only: pathkit cannot record them (LIMITATIONS.md).
package namedboolloop

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Flag bool

// A func literal in the condition: the message must quote the source text,
// so the suggested fix is valid Go.
func FuncLit(ctx workflow.Context, n int) error {
	i := 0
	for func() Flag { return i < n }() {
		_ = workflow.Sleep(ctx, time.Second)
		i++
	}
	return nil
}

// A type parameter constrained by ~bool: not a named type, so the message
// must not call it one.
func TypeParam[T ~bool](ctx workflow.Context, v T) error {
	for !v {
		_ = workflow.Sleep(ctx, time.Second)
		v = true
	}
	return nil
}
