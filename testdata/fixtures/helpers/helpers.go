// Package helpers has workflows that call helper functions of their own
// package: functions that call the Temporal SDK (docs/DESIGN.md D15). The
// decisions inside a helper are decisions of every workflow that calls it,
// one copy per call.
package helpers

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

// charge fails exactly when its activity fails.
func charge(ctx workflow.Context, amount int) error {
	f := workflow.ExecuteActivity(ctx, "Charge", amount)
	if err := f.Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// OneHelper calls a helper that has an error check of its own.
func OneHelper(ctx workflow.Context, amount int) error {
	err := charge(ctx, amount)
	if err != nil {
		return err
	}
	return workflow.Sleep(ctx, time.Second)
}

// TwoCalls calls the same helper twice: two copies, four decisions.
func TwoCalls(ctx workflow.Context) error {
	if err := charge(ctx, 1); err != nil {
		return err
	}
	if err := charge(ctx, 2); err != nil {
		return err
	}
	return nil
}

// pay has a branch of its own before the activity.
func pay(ctx workflow.Context, big bool) error {
	if big {
		if err := workflow.Sleep(ctx, time.Hour); err != nil {
			return err
		}
	}
	return workflow.ExecuteActivity(ctx, "Pay").Get(ctx, nil)
}

// BranchHelper: the helper's branch is a branch of the workflow; its last
// line returns the activity's own error, which the caller checks.
func BranchHelper(ctx workflow.Context, big bool) error {
	if err := pay(ctx, big); err != nil {
		return fmt.Errorf("pay: %w", err)
	}
	return nil
}

// third, second and first call each other in a line, three deep.
func third(ctx workflow.Context) error {
	if err := workflow.Sleep(ctx, time.Second); err != nil {
		return err
	}
	return nil
}

func second(ctx workflow.Context) error {
	return third(ctx)
}

func first(ctx workflow.Context) error {
	if err := second(ctx); err != nil {
		return err
	}
	return nil
}

// ThreeDeep reaches a Sleep through three helpers.
func ThreeDeep(ctx workflow.Context) error {
	return first(ctx)
}

// four is one step too deep: its decisions are not followed.
func four(ctx workflow.Context, big bool) error {
	if big {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

func three(ctx workflow.Context, big bool) error { return four(ctx, big) }
func two(ctx workflow.Context, big bool) error   { return three(ctx, big) }
func one(ctx workflow.Context, big bool) error {
	if big {
		return two(ctx, big)
	}
	return nil
}

func FourDeep(ctx workflow.Context, big bool) error {
	if err := one(ctx, big); err != nil {
		return err
	}
	return nil
}

// loopy calls itself: the recursion is not followed.
func loopy(ctx workflow.Context, n int) error {
	if n == 0 {
		return nil
	}
	if err := workflow.Sleep(ctx, time.Second); err != nil {
		return err
	}
	return loopy(ctx, n-1)
}

func Recursive(ctx workflow.Context) error {
	if err := loopy(ctx, 3); err != nil {
		return err
	}
	return nil
}

type orders struct{ id int }

func (o *orders) ship(ctx workflow.Context) error {
	if o.id == 0 {
		return errors.New("no id")
	}
	return workflow.ExecuteActivity(ctx, "Ship").Get(ctx, nil)
}

// MethodHelper: a method of a known type is followed.
func MethodHelper(ctx workflow.Context) error {
	o := &orders{id: 1}
	if err := o.ship(ctx); err != nil {
		return err
	}
	return nil
}

// ClosureHelper: a function value set once to a function literal is followed.
func ClosureHelper(ctx workflow.Context) error {
	notify := func(ctx workflow.Context) error {
		return workflow.ExecuteActivity(ctx, "Notify").Get(ctx, nil)
	}
	if err := notify(ctx); err != nil {
		return err
	}
	return nil
}

// validate makes decisions but never touches the workflow API: ordinary code.
func validate(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

// PureHelper: nothing changes for a helper that never calls the SDK.
func PureHelper(ctx workflow.Context, n int) error {
	if err := validate(n); err != nil {
		return err
	}
	return workflow.Sleep(ctx, time.Second)
}

// quiet returns whatever error a function value gives: not followed.
func quiet(ctx workflow.Context, f func() error) error {
	_ = workflow.Sleep(ctx, time.Second)
	return f()
}

// UnknownError: the helper's error is not one of its own calls' errors.
func UnknownError(ctx workflow.Context, f func() error) error {
	if err := quiet(ctx, f); err != nil {
		return err
	}
	return nil
}

// calm never returns an error.
func calm(ctx workflow.Context) error {
	_ = workflow.Sleep(ctx, time.Second)
	return nil
}

// NeverFails: a helper that cannot fail leaves the check after it alone.
func NeverFails(ctx workflow.Context) error {
	if err := calm(ctx); err != nil {
		return err
	}
	return nil
}

// each runs an activity for every item.
func each(ctx workflow.Context, items []string) error {
	for range items {
		if err := workflow.ExecuteActivity(ctx, "Item").Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}

// LoopHelper: a loop inside a helper is a loop of the workflow.
func LoopHelper(ctx workflow.Context, items []string) error {
	return each(ctx, items)
}

// InLoop calls a helper from a loop: the loop is now a decision.
func InLoop(ctx workflow.Context, items []string) error {
	for range items {
		if err := charge(ctx, 1); err != nil {
			return err
		}
	}
	return nil
}

type notifier interface {
	Notify(ctx workflow.Context) error
}

// OpaqueCalls: a call through an interface, or through a function value that
// is not set once to a function literal, is not followed.
func OpaqueCalls(ctx workflow.Context, n notifier, f func(workflow.Context) error) error {
	if err := n.Notify(ctx); err != nil {
		return err
	}
	if err := f(ctx); err != nil {
		return err
	}
	return nil
}

// InsideExpression: a call that is part of a bigger expression is not
// followed (nothing says what runs between it and the rest).
func InsideExpression(ctx workflow.Context) error {
	if charge(ctx, 1) == nil {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// Explosion calls a helper eleven times and ignores what it says: every
// call doubles the paths (2 to the 11th is more than the path cap), so the
// helper is not followed in this workflow.
func Explosion(ctx workflow.Context) error {
	_ = charge(ctx, 1)
	_ = charge(ctx, 2)
	_ = charge(ctx, 3)
	_ = charge(ctx, 4)
	_ = charge(ctx, 5)
	_ = charge(ctx, 6)
	_ = charge(ctx, 7)
	_ = charge(ctx, 8)
	_ = charge(ctx, 9)
	_ = charge(ctx, 10)
	_ = charge(ctx, 11)
	return nil
}

func validateAmount(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

// checked has an error check on a call that is not Temporal's: PathKit
// assumes it passes, so what checked returns is not certain.
func checked(ctx workflow.Context, n int) error {
	_ = workflow.Sleep(ctx, time.Millisecond)
	if err := validateAmount(n); err != nil {
		return err
	}
	return nil
}

// AssumedCheck: the check after checked keeps both exits.
func AssumedCheck(ctx workflow.Context, n int) error {
	if err := checked(ctx, n); err != nil {
		return nil
	}
	return workflow.Sleep(ctx, 2*time.Millisecond)
}

// cleared returns an error that a deferred function may replace with nil.
func cleared(ctx workflow.Context, b bool) (err error) {
	defer func() {
		if b {
			err = nil
		}
	}()
	_ = workflow.Sleep(ctx, time.Millisecond)
	return errors.New("x")
}

// DeferredNil: the check after cleared keeps both exits.
func DeferredNil(ctx workflow.Context, b bool) error {
	if err := cleared(ctx, b); err != nil {
		return nil
	}
	return workflow.Sleep(ctx, 2*time.Millisecond)
}
