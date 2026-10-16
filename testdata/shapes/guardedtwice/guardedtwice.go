// Package guardedtwice is shape (d): a value an early return guarantees is
// not empty, checked again in another form.
package guardedtwice

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

type Order struct{ Item string }

// CATCH (written the same way twice): the second check cannot be true.
func SameCheckTwice(ctx workflow.Context, order Order) error {
	if order.Item == "" {
		return nil
	}
	_ = act(ctx)
	if order.Item == "" {
		_ = act(ctx)
	}
	return nil
}

// CATCH (written differently): len(...) == 0 says the same thing.
func LengthCheckAfterGuard(ctx workflow.Context, order Order) error {
	if order.Item == "" {
		return nil
	}
	_ = act(ctx)
	if len(order.Item) == 0 {
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: the item is changed by an unknown call in between.
func ChangedInBetween(ctx workflow.Context, order Order) error {
	item := order.Item
	if item == "" {
		return nil
	}
	item = normalise(item)
	if len(item) == 0 {
		_ = act(ctx)
	}
	return nil
}

func normalise(s string) string { return s }
