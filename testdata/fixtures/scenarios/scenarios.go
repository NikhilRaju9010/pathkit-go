// Package scenarios has workflows whose paths differ in ways that do, or do
// not, change what the workflow does outside itself: calls to activities,
// timers, signals. Paths that make the same calls are one scenario.
package scenarios

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Input is what the workflows are started with.
type Input struct {
	Big, Urgent, Retry bool
	Note               string
	Count              int
}

// The activities do nothing; the workflows are what this fixture is about.
func Charge(context.Context) error    { return nil }
func ChargeBig(context.Context) error { return nil }
func A(context.Context) error         { return nil }
func B(context.Context) error         { return nil }
func C(context.Context) error         { return nil }
func D(context.Context) error         { return nil }
func E(context.Context) error         { return nil }

// options gives the context what a workflow needs to start an activity.
func options(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

func charge(ctx workflow.Context) error {
	return workflow.ExecuteActivity(ctx, Charge).Get(ctx, nil)
}

// Logic has two decisions that only compute: whatever they decide, the
// workflow calls Charge and ends. Four paths, one scenario.
func Logic(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	label := "plain"
	if in.Big {
		label = "big"
	}
	if in.Urgent && label == "big" {
		label = "urgent"
	}
	_ = label
	if err := workflow.ExecuteActivity(ctx, Charge).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Different has a decision that chooses between two activities: its two paths
// are two scenarios (the near miss of Logic).
func Different(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	if in.Big {
		if err := workflow.ExecuteActivity(ctx, ChargeBig).Get(ctx, nil); err != nil {
			return err
		}
		return nil
	}
	if err := workflow.ExecuteActivity(ctx, Charge).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// OrderMatters calls the same two activities in a different order on its two
// paths: two scenarios.
func OrderMatters(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	if in.Big {
		if err := workflow.ExecuteActivity(ctx, A).Get(ctx, nil); err != nil {
			return err
		}
		if err := workflow.ExecuteActivity(ctx, B).Get(ctx, nil); err != nil {
			return err
		}
		return nil
	}
	if err := workflow.ExecuteActivity(ctx, B).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, A).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Wide has fourteen decisions that only compute, then one activity: 32768
// paths with the failure of the activity, but only a few different things it
// does. The main number is scenario coverage.
func Wide(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	n := 0
	if in.Count > 1 {
		n++
	}
	if in.Count > 2 {
		n++
	}
	if in.Count > 3 {
		n++
	}
	if in.Count > 4 {
		n++
	}
	if in.Count > 5 {
		n++
	}
	if in.Count > 6 {
		n++
	}
	if in.Count > 7 {
		n++
	}
	if in.Count > 8 {
		n++
	}
	if in.Count > 9 {
		n++
	}
	if in.Count > 10 {
		n++
	}
	if in.Count > 11 {
		n++
	}
	if in.Count > 12 {
		n++
	}
	if in.Count > 13 {
		n++
	}
	if in.Count > 14 {
		n++
	}
	_ = n
	if err := workflow.ExecuteActivity(ctx, Charge).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Many calls five activities in a row, each of which can fail: six paths,
// six scenarios (no two paths make the same calls).
func Many(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	if err := workflow.ExecuteActivity(ctx, A).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, B).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, C).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, D).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, E).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Events waits for signals in a loop that never ends by itself: its main
// number is handler coverage.
func Events(ctx workflow.Context) error {
	pause := workflow.GetSignalChannel(ctx, "pause")
	stop := workflow.GetSignalChannel(ctx, "stop")
	for {
		sel := workflow.NewSelector(ctx)
		done := false
		sel.AddReceive(pause, func(c workflow.ReceiveChannel, more bool) {
			c.Receive(ctx, nil)
		})
		sel.AddReceive(stop, func(c workflow.ReceiveChannel, more bool) {
			c.Receive(ctx, nil)
			done = true
		})
		sel.Select(ctx)
		if done {
			return nil
		}
	}
}

// Sleeper waits, then calls an activity, so a timer is one of its steps.
func Sleeper(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	if in.Big {
		if err := workflow.Sleep(ctx, time.Second); err != nil {
			return err
		}
	}
	return charge(ctx)
}
