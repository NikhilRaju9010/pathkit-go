// Package dataflowget reproduces the "result filled in by Get(ctx, &res)" shape.
package dataflowget

import (
	"go.temporal.io/sdk/workflow"
	"time"
)

type Res struct {
	Ok   bool
	Name string
}

type holder struct{ res Res }

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }
func fill(r *Res)                    {}

func get(ctx workflow.Context, name string, out any) error {
	return workflow.ExecuteActivity(ctx, name).Get(ctx, out)
}

// CATCH: one Get, then the same field tested twice with an unrelated
// activity in between.
func CatchOneGet(ctx workflow.Context) error {
	var res Res
	if err := workflow.ExecuteActivity(ctx, "A").Get(ctx, &res); err != nil {
		return err
	}
	if res.Ok {
		_ = act(ctx)
	}
	var other Res
	_ = workflow.ExecuteActivity(ctx, "B").Get(ctx, &other)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// CATCH: the Get happens in a loop, both tests come after the loop.
func CatchGetInLoopTestsAfter(ctx workflow.Context, n int) error {
	var res Res
	for i := 0; i < n; i++ {
		_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	}
	if res.Ok {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a second Get into the same variable between the tests.
func SecondGetBetween(ctx workflow.Context) error {
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	_ = workflow.ExecuteActivity(ctx, "B").Get(ctx, &res)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the address goes to another function.
func AddressPassedOn(ctx workflow.Context) error {
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	fill(&res)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the Get target is a field of a struct.
func StructField(ctx workflow.Context) error {
	var h holder
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &h.res)
	if h.res.Ok {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !h.res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a closure captures the variable and writes it.
func ClosureCapture(ctx workflow.Context) error {
	var res Res
	reset := func() { res.Ok = false }
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	reset()
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the Get is in a loop that also contains one of the tests.
func LoopReGet(ctx workflow.Context, n int) error {
	var res Res
	for i := 0; i < n; i++ {
		if res.Ok {
			_ = act(ctx)
		}
		_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	}
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a plain write between the tests.
func WriteBetween(ctx workflow.Context) error {
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	res.Ok = false
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the address goes through a helper that calls Get.
func ViaHelper(ctx workflow.Context) error {
	var res Res
	_ = get(ctx, "A", &res)
	if res.Ok {
		_ = act(ctx)
	}
	_ = get(ctx, "B", &res)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: different fields of the same result.
func DifferentFields(ctx workflow.Context) error {
	var res Res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	if res.Name == "x" {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a pointer to the result stored in a slice.
func PointerStored(ctx workflow.Context) error {
	var res Res
	ptrs := []*Res{&res}
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	ptrs[0].Ok = false
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the Get target is chosen at run time between two variables.
func ChosenTarget(ctx workflow.Context, flag bool) error {
	var res, other Res
	var out any = &res
	if flag {
		out = &other
	}
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, out)
	if res.Ok {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: a deferred closure writes it.
func DeferredWrite(ctx workflow.Context) error {
	var res Res
	defer func() { res.Ok = true }()
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, &res)
	if res.Ok {
		_ = act(ctx)
	}
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}

// CATCH (same as CatchOneGet, target held in an interface variable once).
func CatchInterfaceOnce(ctx workflow.Context) error {
	var res Res
	var out any = &res
	_ = workflow.ExecuteActivity(ctx, "A").Get(ctx, out)
	if res.Ok {
		_ = act(ctx)
	}
	_ = act(ctx)
	if !res.Ok {
		_ = act(ctx)
	}
	return nil
}
