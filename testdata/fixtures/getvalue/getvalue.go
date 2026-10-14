// Package getvalue holds the shapes of the detector
// "contradictory-get-result": a value Temporal fills in with
// Get(ctx, &res) and the workflow then tests twice. CATCH workflows must be
// marked; every NEAR-MISS must not.
package getvalue

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Res is what the activities return.
type Res struct {
	Ok    bool
	N     int
	P     *Inner
	Inner Inner
	Items []Inner
}

type Inner struct{ Fine bool }

// Clear has a pointer receiver: calling it changes the value.
func (r *Res) Clear() { r.Ok = false }

func opts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

func nap(ctx workflow.Context) { _ = workflow.Sleep(ctx, time.Second) }

func fill(r *Res)      { r.Ok = false }
func fillPtr(r **Res)  { *r = &Res{Ok: false} }
func keep(r *Res) bool { return r != nil }

// CATCH: one Get, then the same field tested twice.
func ValueTwice(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	_ = workflow.Sleep(ctx, time.Second)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// CATCH: a pointer result, as real workflows often have it, tested alone
// and in a conjunction.
func PointerAndConjunction(ctx workflow.Context, other bool) error {
	ctx = opts(ctx)
	var res *Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	if res.Ok && other {
		nap(ctx)
	}
	return nil
}

// CATCH: a field of a nested struct, and a disjunction's false side.
func NestedField(ctx workflow.Context, other bool) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Inner.Fine {
		nap(ctx)
	}
	if !res.Inner.Fine || other {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a second Get into the same variable.
func SecondGet(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	_ = workflow.ExecuteActivity(ctx, "B").Get(ctx, &res)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a plain write between the tests.
func WriteBetween(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	res.Ok = false
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: the address goes to another function, which writes it.
func AddressPassedOn(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	fill(&res)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a pointer-receiver method changes it, with no & written.
func MethodOnResult(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	res.Clear()
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a method value takes the address too.
func MethodValueOnResult(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	clear := res.Clear
	clear()
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: the pointer is copied and written through the copy.
func PointerAlias(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res *Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	alias := res
	alias.Ok = false
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: the pointer is passed to a function that writes through it.
func PointerPassed(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res *Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	fill(res)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a closure writes it.
func ClosureWrites(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	reset := func() { res.Ok = false }
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	reset()
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: one test is before the Get.
func TestBeforeGet(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	if res.Ok {
		nap(ctx)
	}
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a Get in a loop can run again later.
func GetInLoop(ctx workflow.Context, n int) error {
	ctx = opts(ctx)
	var res Res
	for i := 0; i < n; i++ {
		_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	}
	if res.Ok {
		nap(ctx)
	}
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: the Get is deferred and runs after the tests.
func DeferredGet(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	defer func() { _ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res) }()
	if res.Ok {
		nap(ctx)
	}
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a field reached through a slice element.
func ThroughSlice(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if len(res.Items) > 0 && res.Items[0].Fine {
		nap(ctx)
	}
	res.Items[0].Fine = false
	if len(res.Items) > 0 && !res.Items[0].Fine {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: the pointer to the pointer is passed on.
func PointerToPointerPassed(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res *Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	fillPtr(&res)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

var _ = keep

// NEAR-MISS (found by the independent review): a copy of the pointer,
// written in parentheses, then written through.
func ParenAlias(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res *Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	w := (res)
	w.Ok = false
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a counter field is incremented (not an atom, but it must be
// refused as a write to the result).
func IncrementBetween(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	res.N++
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: a bool field reached through a pointer field, changed through
// another name for that pointer.
func ThroughPointerField(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.P != nil && res.P.Fine {
		nap(ctx)
	}
	q := res.P
	if q != nil {
		q.Fine = false
	}
	if res.P != nil && !res.P.Fine {
		nap(ctx)
	}
	return nil
}

// fake has a Get method that is not Temporal's.
type fake struct{ v Res }

func (f *fake) Get(ctx workflow.Context, out *Res) { *out = f.v }

// NEAR-MISS: a Get that is not the Temporal SDK's: it may do anything
// (start goroutines, keep the pointer).
func NotTemporalGet(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	f := &fake{}
	f.Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	if !res.Ok {
		nap(ctx)
	}
	return nil
}

// NEAR-MISS: two Gets into the same variable, both before both tests (the
// address is taken twice).
func TwoGetsBeforeTests(ctx workflow.Context) error {
	ctx = opts(ctx)
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	_ = workflow.ExecuteActivity(ctx, "B").Get(ctx, &res)
	if res.Ok {
		nap(ctx)
	}
	nap(ctx)
	if !res.Ok {
		nap(ctx)
	}
	return nil
}
