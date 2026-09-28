package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func ActivityErr(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	var r string
	err := workflow.ExecuteActivity(ctx, Charge, 10).Get(ctx, &r)
	if err != nil {
		return "", err
	}
	return r, nil
}

func NilOnLeft(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	err := workflow.ExecuteActivity(ctx, "Charge", 10).Get(ctx, nil)
	if nil != err {
		return "", err
	}
	return "ok", nil
}

func EqNilTemporal(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	if err := workflow.ExecuteLocalActivity(ctx, Notify).Get(ctx, nil); err == nil {
		return "sent", nil
	}
	return "", nil
}

func ChildErr(ctx workflow.Context) (string, error) {
	var r string
	err := workflow.ExecuteChildWorkflow(ctx, ChildFlow).Get(ctx, &r)
	if err != nil {
		return "", err
	}
	return r, nil
}

func SleepAwaitErr(ctx workflow.Context) (string, error) {
	if err := workflow.Sleep(ctx, time.Minute); err != nil {
		return "", err
	}
	ready := false
	if err := workflow.Await(ctx, func() bool { return ready }); err != nil {
		return "", err
	}
	return "ok", nil
}

func FutureVariable(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	f := workflow.ExecuteActivity(ctx, Charge, 5)
	var r string
	err := f.Get(ctx, &r)
	if err != nil {
		return "", err
	}
	return r, nil
}

// TransparentPlainErr: decode is not a Temporal call, so its check is not a
// junction. One path.
func TransparentPlainErr(ctx workflow.Context, s string) (string, error) {
	n, err := decode(s)
	if err != nil {
		return "", err
	}
	_ = n
	return "ok", nil
}

// TransparentEqNil: the "no error" side of err == nil is the true side, so
// the if inside it must be seen (2 paths). Skipping it would give 1.
func TransparentEqNil(ctx workflow.Context, s string, flag bool) (string, error) {
	if _, err := decode(s); err == nil {
		if flag {
			return "flagged", nil
		}
	}
	return "plain", nil
}

// QueryHandlerErr: SetQueryHandler is a workflow-package call but not in
// D2's list, so the check is transparent. One path.
func QueryHandlerErr(ctx workflow.Context) (string, error) {
	if err := workflow.SetQueryHandler(ctx, "q", func() (string, error) { return "x", nil }); err != nil {
		return "", err
	}
	return "ok", nil
}

// NearestAssignment: err is reassigned by a Temporal call after a
// non-Temporal one; the check uses the nearest assignment (Temporal).
func NearestAssignment(ctx workflow.Context, s string) (string, error) {
	ctx = withOptions(ctx)
	_, err := decode(s)
	err = workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
	if err != nil {
		return "", err
	}
	return "ok", nil
}
