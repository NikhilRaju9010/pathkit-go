package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// A saga compensation defer (the deferred call starts an activity, local
// activity or child workflow) is a note on every path that passes it:
// "[compensation (defer)]". It is never a branch (CLAUDE.md D3).

func UsesDeferCompensation(ctx workflow.Context) (err error) {
	ctx = withOptions(ctx)
	defer func() {
		if err != nil {
			_ = workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
		}
	}()
	return nil
}

// A defer that starts nothing is not compensation: no note.
func PlainDefer(ctx workflow.Context) (string, error) {
	cleanup := func() {}
	defer cleanup()
	return "ok", nil
}

// The note appears only on paths that get past the defer.
func SagaTwoSteps(ctx workflow.Context) (err error) {
	ctx = withOptions(ctx)
	if err = workflow.ExecuteActivity(ctx, Charge, 1).Get(ctx, nil); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
		}
	}()
	if err = workflow.ExecuteChildWorkflow(ctx, ChildFlow).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// Deferring the activity call itself counts too.
func DeferDirectActivity(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	defer workflow.ExecuteActivity(ctx, Notify)
	return "ok", nil
}

// defer cancel() and a deferred log call start nothing: no note.
func DeferCancelAndLog(ctx workflow.Context) (string, error) {
	ctx, cancel := workflow.WithCancel(ctx)
	defer cancel()
	defer workflow.GetLogger(ctx).Info("done")
	return "ok", nil
}

// A defer inside a selector callback runs when the callback ends, not
// when the workflow ends: no note.
func DeferInCallback(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {
		defer workflow.ExecuteActivity(ctx, Notify)
	})
	sel.Select(ctx)
	return "ok", nil
}

// A defer registered inside a loop: the note is on the paths that reach it.
func DeferInLoop(ctx workflow.Context, n int) error {
	ctx = withOptions(ctx)
	for i := 0; i < n; i++ {
		if err := workflow.ExecuteActivity(ctx, Charge, i).Get(ctx, nil); err != nil {
			return err
		}
		defer func() {
			_ = workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
		}()
	}
	return nil
}
