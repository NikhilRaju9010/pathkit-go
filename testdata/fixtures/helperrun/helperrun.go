// Package helperrun has workflows that call helper functions and that really
// run in the tests, so "pathkit test" records the decisions made inside the
// helpers (docs/DESIGN.md D15).
package helperrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Fail names the activities that fail in a test run.
var (
	Fail   = map[string]bool{}
	failMu sync.Mutex
)

func SetFail(names ...string) {
	failMu.Lock()
	defer failMu.Unlock()
	Fail = map[string]bool{}
	for _, n := range names {
		Fail[n] = true
	}
}

func act(name string) error {
	failMu.Lock()
	defer failMu.Unlock()
	if Fail[name] {
		return errors.New(name + " failed")
	}
	return nil
}

func Charge(context.Context, int) error { return act("Charge") }
func Ship(context.Context) error        { return act("Ship") }
func Notify(context.Context) error      { return act("Notify") }
func Item(context.Context) error        { return act("Item") }

func options(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// charge fails exactly when its activity fails.
func charge(ctx workflow.Context, amount int) error {
	f := workflow.ExecuteActivity(ctx, Charge, amount)
	if err := f.Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// OneHelper calls a helper that checks its own activity.
func OneHelper(ctx workflow.Context, amount int) error {
	ctx = options(ctx)
	err := charge(ctx, amount)
	if err != nil {
		return err
	}
	return workflow.Sleep(ctx, time.Second)
}

// TwoCalls calls the same helper twice: two copies, each its own decisions.
func TwoCalls(ctx workflow.Context) error {
	ctx = options(ctx)
	if err := charge(ctx, 1); err != nil {
		return err
	}
	if err := charge(ctx, 2); err != nil {
		return err
	}
	return nil
}

// ship has a branch of its own before its activity.
func ship(ctx workflow.Context, express bool) error {
	if express {
		if err := workflow.Sleep(ctx, time.Hour); err != nil {
			return err
		}
	}
	return workflow.ExecuteActivity(ctx, Ship).Get(ctx, nil)
}

// BranchHelper: the helper's branch and the activity's own error that it
// returns are both steps of the workflow.
func BranchHelper(ctx workflow.Context, express bool) error {
	ctx = options(ctx)
	if err := ship(ctx, express); err != nil {
		return fmt.Errorf("ship: %w", err)
	}
	return nil
}

func third(ctx workflow.Context, n int) error {
	if n > 0 {
		return workflow.ExecuteActivity(ctx, Item).Get(ctx, nil)
	}
	return nil
}

func second(ctx workflow.Context, n int) error {
	if err := third(ctx, n); err != nil {
		return err
	}
	return workflow.Sleep(ctx, time.Second)
}

func first(ctx workflow.Context, n int) error { return second(ctx, n) }

// ThreeDeep reaches a decision through three helpers.
func ThreeDeep(ctx workflow.Context, n int) error {
	ctx = options(ctx)
	return first(ctx, n)
}

type shop struct{ open bool }

func (s *shop) notify(ctx workflow.Context) error {
	if !s.open {
		return errors.New("closed")
	}
	return workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
}

// MethodHelper: a method of a known type is followed.
func MethodHelper(ctx workflow.Context, open bool) error {
	ctx = options(ctx)
	s := &shop{open: open}
	if err := s.notify(ctx); err != nil {
		return err
	}
	return nil
}

// ClosureHelper: a function value set once to a literal is followed.
func ClosureHelper(ctx workflow.Context, loud bool) error {
	ctx = options(ctx)
	tell := func(ctx workflow.Context) error {
		if loud {
			return workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
		}
		return nil
	}
	if err := tell(ctx); err != nil {
		return err
	}
	return nil
}

func each(ctx workflow.Context, items []string) error {
	for range items {
		if err := workflow.ExecuteActivity(ctx, Item).Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}

// LoopHelper: a loop inside a helper is a loop of the workflow.
func LoopHelper(ctx workflow.Context, items []string) error {
	ctx = options(ctx)
	return each(ctx, items)
}

// InLoop calls a helper from a loop.
func InLoop(ctx workflow.Context, items []string) error {
	ctx = options(ctx)
	for range items {
		if err := charge(ctx, 1); err != nil {
			return err
		}
	}
	return nil
}

func validate(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

// PureHelper: a helper that never calls the SDK is ordinary code.
func PureHelper(ctx workflow.Context, n int) error {
	if err := validate(n); err != nil {
		return err
	}
	return workflow.Sleep(ctx, time.Second)
}
