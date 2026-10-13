// Package dataflow is a minimal reproduction of the two simple data-flow patterns.
package dataflow

import (
	"strconv"
	"time"

	"go.temporal.io/sdk/workflow"
)

type In struct {
	Items []string
	Other []string
	Flag  bool
}

type Res struct{ Ok bool }

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func validate(in In) bool { return in.Flag }

// ===== pattern 1: the same result tested twice =====

// CATCH: a pure value (computed once), tested twice.
func TwiceLocal(ctx workflow.Context, in In) error {
	valid := len(in.Items) > 0
	if valid {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !valid {
		_ = act(ctx)
	}
	return nil
}

// CATCH: a field of an input value that nobody writes.
func TwiceField(ctx workflow.Context, in In) error {
	if in.Flag {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !in.Flag {
		_ = act(ctx)
	}
	return nil
}

// MISS (needs memory reasoning): a result filled in by Get(&res).
func TwiceViaGet(ctx workflow.Context) error {
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	_ = workflow.ExecuteActivity(ctx, "B").Get(ctx, nil)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: reassigned in between.
func Reassigned(ctx workflow.Context, in In) error {
	ok := in.Flag
	if ok {
		_ = act(ctx)
	}
	ok = len(in.Items) > 0
	if !ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the test is a call made twice (could answer differently).
func TwoCalls(ctx workflow.Context, in In) error {
	if validate(in) {
		_ = act(ctx)
	}
	if !validate(in) {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: input field written in between.
func FieldWritten(ctx workflow.Context, in In) error {
	if in.Flag {
		_ = act(ctx)
	}
	in.Flag = false
	if !in.Flag {
		_ = act(ctx)
	}
	return nil
}

// ===== pattern 2: two loops over the same list =====
// (a loop is a junction on the map only when its body calls the Temporal
// SDK directly, so these loops call workflow.Sleep themselves)

// CATCH
func TwoLoops(ctx workflow.Context, in In) error {
	for range in.Items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range in.Items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// CATCH: the same local variable
func TwoLoopsLocal(ctx workflow.Context, in In) error {
	items := in.Items
	for _, it := range items {
		_ = it
		_ = workflow.Sleep(ctx, time.Second)
	}
	for _, it := range items {
		_ = it
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: the list changes in between
func ListAppended(ctx workflow.Context, in In) error {
	items := in.Items
	for range items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	items = append(items, "x")
	for range items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: a different list
func DifferentLists(ctx workflow.Context, in In) error {
	for range in.Items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range in.Other {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: the first loop changes the list it walks
func GrowsInLoop(ctx workflow.Context, in In) error {
	items := in.Items
	for i := range items {
		_ = i
		items = append(items, "x")
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: a closure writes the input between the tests.
func ClosureWrites(ctx workflow.Context, in In) error {
	clear := func() { in.Flag = false }
	if in.Flag {
		_ = act(ctx)
	}
	clear()
	if !in.Flag {
		_ = act(ctx)
	}
	return nil
}

// ===== more shapes, added when the detectors were built =====

// CATCH: the same condition tested twice (same side): (true,false) and
// (false,true) cannot happen.
func SameConditionTwice(ctx workflow.Context, in In) error {
	if len(in.Items) > 3 {
		_ = act(ctx)
	}
	_ = act(ctx)
	if len(in.Items) > 3 {
		_ = act(ctx)
	}
	return nil
}

// CATCH: else-if on the opposite of the first test: "neither" is impossible.
func ElseIfOpposite(ctx workflow.Context, in In) error {
	valid := in.Flag
	if valid {
		_ = act(ctx)
	} else if !valid {
		_ = act(ctx)
	}
	return nil
}

// CATCH: the list is a parameter.
func RangeParamTwice(ctx workflow.Context, items []string) error {
	for range items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range items {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: both tests are inside a loop (different trips can differ).
func TestsInsideLoop(ctx workflow.Context, in In) error {
	for range in.Items {
		if in.Flag {
			_ = act(ctx)
		}
		_ = act(ctx)
		if !in.Flag {
			_ = act(ctx)
		}
	}
	return nil
}

// NEAR-MISS: a map's length can change between the loops.
func MapRangeTwice(ctx workflow.Context, m map[string]int) error {
	for range m {
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range m {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: a received value is a new value each time.
func ChannelReceiveTwice(ctx workflow.Context, flags chan bool) error {
	if <-flags {
		_ = act(ctx)
	}
	if !<-flags {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a field read through a pointer can change under the workflow.
func PointerParamField(ctx workflow.Context, in *In) error {
	if in.Flag {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !in.Flag {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: two loops over a re-sliced list are two different values.
func ResliceTwice(ctx workflow.Context, in In) error {
	for range in.Items[1:] {
		_ = workflow.Sleep(ctx, time.Second)
	}
	for range in.Items[1:] {
		_ = workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NEAR-MISS: a compound condition is its own value.
func CompoundTwice(ctx workflow.Context, in In) error {
	if in.Flag && len(in.Items) > 0 {
		_ = act(ctx)
	}
	if !in.Flag || len(in.Items) == 0 {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a whole struct assigned twice.
func WholeStructReassigned(ctx workflow.Context) error {
	x := In{Flag: true}
	if x.Flag {
		_ = act(ctx)
	}
	x = In{}
	if !x.Flag {
		_ = act(ctx)
	}
	return nil
}

var keep []*In

// NEAR-MISS: the address is stored, then written through the pointer.
func AddressStored(ctx workflow.Context, in In) error {
	if in.Flag {
		_ = act(ctx)
	}
	keep = append(keep, &in)
	keep[0].Flag = false
	if !in.Flag {
		_ = act(ctx)
	}
	return nil
}

// ===== found by the independent review of the detectors =====

// NEAR-MISS: two different long string constants (the old key abbreviated
// both to the same text).
func LongStrings(ctx workflow.Context, status string) error {
	if status == "ORDER_STATUS_PENDING_PAYMENT" {
		_ = act(ctx)
	}
	_ = act(ctx)
	if status == "ORDER_STATUS_PENDING_APPROVAL" {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: two float constants that print the same with six digits.
func FloatConstants(ctx workflow.Context, x float64) error {
	if x > 0.1234567 {
		_ = act(ctx)
	}
	_ = act(ctx)
	if x > 0.1234568 {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the length of a map changes when it is written to.
func MapLength(ctx workflow.Context, m map[string]int) error {
	if len(m) == 0 {
		m["a"] = 1
	}
	_ = act(ctx)
	if len(m) == 0 {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the length of a channel changes as values are sent.
func ChannelLength(ctx workflow.Context, ch chan int) error {
	if len(ch) == 0 {
		_ = act(ctx)
	}
	_ = act(ctx)
	if len(ch) != 0 {
		_ = act(ctx)
	}
	return nil
}

type S2 struct{ A bool }
type Box struct{ S S2 }

// NEAR-MISS: zero at first, assigned once later: the early read sees zero.
func ZeroThenStore(ctx workflow.Context, in Box) error {
	var s S2
	if s.A {
		_ = act(ctx)
	}
	s = in.S
	if !s.A {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the single assignment is in a branch.
func StoreInBranch(ctx workflow.Context, in Box) error {
	var s S2
	if s.A {
		_ = act(ctx)
	}
	if in.S.A {
		s = in.S
	}
	if !s.A {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a constant that is 64 on this machine but 32 on another.
func IntSizePair(ctx workflow.Context, n int) error {
	if n > strconv.IntSize {
		_ = act(ctx)
	}
	if n > 64 {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a constant that differs by operating system.
func LimitPair(ctx workflow.Context, n int) error {
	if n > LimitA {
		_ = act(ctx)
	}
	if n > 10 {
		_ = act(ctx)
	}
	return nil
}
