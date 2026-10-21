// Package notunderstood is safety fixture: a value assigned something PathKit
// does not understand (a call result, arithmetic, ++) is unknown afterwards.
// Must stay testable.
package notunderstood

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func normalise(s string) string { return s + "" }

func CallResult(ctx workflow.Context, name string) error {
	a := ""
	a = normalise(name)
	if a == "" {
		_ = act(ctx)
	}
	return nil
}

func Arithmetic(ctx workflow.Context, name string) error {
	b := 0
	b += len(name)
	if b == 0 {
		_ = act(ctx)
	}
	return nil
}

func Increment(ctx workflow.Context) error {
	c := 0
	c++
	if c == 0 {
		_ = act(ctx)
	}
	return nil
}

func ActivityResult(ctx workflow.Context) error {
	var res string
	var err error
	res, err = activityLike(ctx)
	if err != nil {
		return err
	}
	if res == "" {
		_ = act(ctx)
	}
	return nil
}

// activityLike stands for a call whose result PathKit cannot know (an activity).
// It goes through a function value on purpose: a plain function with a visible
// body is read and summarized, and this one must stay unknown.
var activityLikeFn = func(ctx workflow.Context) (string, error) { return "", nil }

func activityLike(ctx workflow.Context) (string, error) { return activityLikeFn(ctx) }
