// Package builtlist holds the shapes of the detector "list-built-from-list":
// a second loop walks a list that the first loop builds, one append per
// trip. The CATCH workflows must be marked; every NEAR-MISS must not.
package builtlist

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type In struct {
	Items []string
	Seed  []string
	Reset bool
}

func timer(ctx workflow.Context) workflow.Future { return workflow.NewTimer(ctx, time.Second) }

func fill(l *[]workflow.Future) {}

// CATCH: the first loop appends one future per item, the second waits.
func BuiltTwoLoops(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for range in.Items {
		f := workflow.NewTimer(ctx, time.Second)
		timers = append(timers, f)
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// CATCH: the list starts as make(..., 0, n) and has a statement before the
// append that cannot leave the body.
func BuiltWithMake(ctx workflow.Context, in In) error {
	timers := make([]workflow.Future, 0, len(in.Items))
	for _, it := range in.Items {
		name := it + "!"
		_ = name
		f := workflow.NewTimer(ctx, time.Second)
		timers = append(timers, f)
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: the append is in an if, so a trip can add nothing.
func ConditionalAppend(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for _, it := range in.Items {
		if it != "" {
			timers = append(timers, workflow.NewTimer(ctx, time.Second))
		}
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: a continue before the append.
func ContinueBeforeAppend(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for _, it := range in.Items {
		if it == "" {
			continue
		}
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: the list does not start empty.
func StartsNotEmpty(ctx workflow.Context, in In) error {
	timers := []workflow.Future{timer(ctx)}
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: another append between the loops.
func AppendedBetween(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	timers = append(timers, timer(ctx))
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: the list is replaced between the loops.
func ReplacedBetween(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	if len(in.Seed) > 0 {
		timers = []workflow.Future{timer(ctx)}
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: a closure also appends to the list.
func ClosureAppends(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	add := func() { timers = append(timers, timer(ctx)) }
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	if len(in.Seed) > 0 {
		add()
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: the address of the list is passed on.
func AddressPassed(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	if len(in.Seed) > 0 {
		fill(&timers)
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: both loops sit inside another loop (another trip may see
// another list).
func InsideOuterLoop(ctx workflow.Context, in In) error {
	for range in.Seed {
		var timers []workflow.Future
		for range in.Items {
			timers = append(timers, workflow.NewTimer(ctx, time.Second))
		}
		for _, f := range timers {
			_ = f.Get(ctx, nil)
		}
	}
	return nil
}

// NEAR-MISS: the second loop comes first.
func WalkedBeforeBuilt(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	return nil
}

// NEAR-MISS: a map is not a list (its range is not claimed).
func BuiltFromMap(ctx workflow.Context, m map[string]int) error {
	var timers []workflow.Future
	for range m {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS: the appending loop can return before the append.
func ReturnBeforeAppend(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for _, it := range in.Items {
		if it == "stop" {
			return nil
		}
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// Futs is a named list type with a pointer-receiver method.
type Futs []workflow.Future

func (f *Futs) Clear() { *f = nil }

// NEAR-MISS (found by the independent review): a pointer-receiver method
// takes the address of the list without writing &.
func MethodReset(ctx workflow.Context, in In) error {
	var timers Futs
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	if in.Reset {
		timers.Clear()
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS (same review): a method value does the same.
func MethodValueReset(ctx workflow.Context, in In) error {
	var timers Futs
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	clear := timers.Clear
	if in.Reset {
		clear()
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}

// NEAR-MISS (same review): an address in parentheses.
func ParenAddress(ctx workflow.Context, in In) error {
	var timers []workflow.Future
	for range in.Items {
		timers = append(timers, workflow.NewTimer(ctx, time.Second))
	}
	p := &(timers)
	if in.Reset {
		*p = nil
	}
	for _, f := range timers {
		_ = f.Get(ctx, nil)
	}
	return nil
}
