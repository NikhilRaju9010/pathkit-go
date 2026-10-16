// Package factsengine holds small made-up workflows for the fact engine
// (internal/facts): each fact source, and each case where the engine must
// forget. A workflow whose name starts with Impossible has a path the
// engine proves impossible; one that starts with Possible has none. The
// exact claims are listed in internal/facts/engine_test.go.
package factsengine

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func unknownString(s string) string { return s + "" }

type Input struct {
	Name  string
	Count int
	Flag  bool
	Inner struct{ ID string }
	Ptr   *Input
}

type Flagged bool

// ---- fact sources ----

// A declared string is "" until something assigns it.
func ImpossibleDeclaredString(ctx workflow.Context) error {
	var s string
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

// Numbers and bools start at zero / false, pointers at nil.
func ImpossibleDeclaredOthers(ctx workflow.Context) error {
	var n int
	var b bool
	var p *Input
	if n > 0 {
		_ = act(ctx)
	}
	if b {
		_ = act(ctx)
	}
	if p != nil {
		_ = act(ctx)
	}
	return nil
}

// Every field of a declared struct starts at its zero value, nested too.
func ImpossibleDeclaredStruct(ctx workflow.Context) error {
	var in Input
	if in.Name != "" {
		_ = act(ctx)
	}
	if in.Inner.ID != "" {
		_ = act(ctx)
	}
	return nil
}

// A constant assignment.
func ImpossibleConstAssigned(ctx workflow.Context) error {
	x := "a"
	if x == "b" {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleConstReassigned(ctx workflow.Context, in Input) error {
	x := in.Name
	x = "done"
	if x != "done" {
		_ = act(ctx)
	}
	return nil
}

// &T{}, new and make are never nil.
func ImpossibleNonNil(ctx workflow.Context) error {
	p := &Input{}
	q := new(int)
	m := make(map[string]int)
	if p == nil {
		_ = act(ctx)
	}
	if q == nil {
		_ = act(ctx)
	}
	if m == nil {
		_ = act(ctx)
	}
	return nil
}

// A branch outcome: x == "a" true, then x == "b" true cannot both hold.
func ImpossibleBranchContradiction(ctx workflow.Context, x string) error {
	if x == "a" {
		_ = act(ctx)
	}
	if x == "b" {
		_ = act(ctx)
	}
	return nil
}

// x != "a" false means x is "a".
func ImpossibleNotEqualFalse(ctx workflow.Context, x string) error {
	if x != "a" {
		_ = act(ctx)
	}
	if x == "b" {
		_ = act(ctx)
	}
	return nil
}

// Not-equal facts: x != "a" and x != "b" taken, then x == "a" is false.
func ImpossibleNotEqualSet(ctx workflow.Context, x string) error {
	if x != "a" {
		if x != "b" {
			if x == "a" {
				_ = act(ctx)
			}
		}
	}
	return nil
}

// && taken true teaches both sides; || taken false teaches both sides.
func ImpossibleAndOr(ctx workflow.Context, a, b string) error {
	if a == "x" && b == "y" {
		if a != "x" {
			_ = act(ctx)
		}
		if b != "y" {
			_ = act(ctx)
		}
	}
	if a != "p" || b != "q" {
		_ = act(ctx)
	} else {
		if a != "p" {
			_ = act(ctx)
		}
	}
	return nil
}

// An early return: after it, the path knows the value is not empty.
func ImpossibleEarlyReturn(ctx workflow.Context, in Input) error {
	if in.Name == "" {
		return nil
	}
	_ = act(ctx)
	if in.Name == "" {
		_ = act(ctx)
	}
	return nil
}

// len() forms of the same fact.
func ImpossibleLenForms(ctx workflow.Context, name string) error {
	if len(name) == 0 {
		return nil
	}
	if len(name) == 0 {
		_ = act(ctx)
	}
	if len(name) < 1 {
		_ = act(ctx)
	}
	if len(name) > 0 {
		_ = act(ctx)
	}
	return nil
}

// Comma-ok forms taken false give the zero value.
func ImpossibleCommaOk(ctx workflow.Context, raw interface{}, m map[string]string) error {
	v, ok := raw.(string)
	if !ok {
		if v != "" {
			_ = act(ctx)
		}
	}
	w, found := m["k"]
	if !found {
		if w != "" {
			_ = act(ctx)
		}
	}
	return nil
}

// A copy carries everything known.
func ImpossibleCopy(ctx workflow.Context) error {
	x := ""
	y := x
	if y != "" {
		_ = act(ctx)
	}
	return nil
}

// A copy of a struct carries its fields.
func ImpossibleStructCopy(ctx workflow.Context) error {
	var a Input
	b := a
	if b.Name != "" {
		_ = act(ctx)
	}
	return nil
}

// A field assigned by hand.
func ImpossibleFieldAssigned(ctx workflow.Context, in Input) error {
	in.Name = "fixed"
	if in.Name == "" {
		_ = act(ctx)
	}
	return nil
}

// Named bool types are bools.
func ImpossibleNamedBool(ctx workflow.Context) error {
	var f Flagged
	if f {
		_ = act(ctx)
	}
	return nil
}

// A swap reads both sides before it writes.
func ImpossibleSwap(ctx workflow.Context) error {
	a, b := "x", "y"
	a, b = b, a
	if a != "y" {
		_ = act(ctx)
	}
	if b != "x" {
		_ = act(ctx)
	}
	return nil
}

// ---- three-valued: unknown never claims ----

// The same unchanged value, asked twice: the answers cannot differ, even
// though what the value is stays unknown.
func ImpossibleSameValueTwice(ctx workflow.Context, name string) error {
	x := unknownString(name)
	if x == "" {
		_ = act(ctx)
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// Asked once, an unknown value claims nothing.
func PossibleUnknownOnce(ctx workflow.Context, name string) error {
	x := unknownString(name)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// "unknown && false" is false, but "unknown && true" is unknown.
func ImpossibleUnknownAnd(ctx workflow.Context, name string, other bool) error {
	var s string
	if s != "" && other {
		_ = act(ctx)
	}
	if s == "" && other {
		_ = act(ctx)
	}
	return nil
}

func PossibleOrdered(ctx workflow.Context, n int) error {
	if n > 5 {
		_ = act(ctx)
	}
	if n > 3 {
		_ = act(ctx)
	}
	return nil
}
