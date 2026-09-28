package rules

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// An if on the "did it arrive?" result of AwaitWithTimeout (exits
// signaled / timeout), ReceiveWithTimeout or ReceiveAsync (received /
// not received) is a junction (CLAUDE.md D3).

func UsesAwaitResult(ctx workflow.Context) (string, error) {
	done := false
	ok, err := workflow.AwaitWithTimeout(ctx, time.Hour, func() bool { return done })
	if err != nil {
		return "", err
	}
	if !ok {
		return "timeout", nil
	}
	return "done", nil
}

func UsesReceiveWithTimeout(ctx workflow.Context) (string, error) {
	var v string
	if ok, _ := workflow.GetSignalChannel(ctx, "s").ReceiveWithTimeout(ctx, time.Minute, &v); ok {
		return v, nil
	}
	return "none", nil
}

func ReceiveAsyncInline(ctx workflow.Context) (string, error) {
	var v string
	if !workflow.GetSignalChannel(ctx, "s").ReceiveAsync(&v) {
		return "none", nil
	}
	return v, nil
}

func ReceiveAsyncMoreFlag(ctx workflow.Context) (string, error) {
	var v string
	ok, more := workflow.GetSignalChannel(ctx, "s").ReceiveAsyncWithMoreFlag(&v)
	_ = more
	if ok {
		return v, nil
	}
	return "none", nil
}

// Anything but exactly "ok" / "!ok" is an ordinary true / false junction.
func AwaitCompound(ctx workflow.Context, x int) (string, error) {
	done := false
	ok, _ := workflow.AwaitWithTimeout(ctx, time.Hour, func() bool { return done })
	if ok && x > 0 {
		return "both", nil
	}
	return "not both", nil
}

// Ignored: assumed false, like any ignored if; "!ok" false means signaled.
func AwaitIgnored(ctx workflow.Context) (string, error) {
	done := false
	ok, _ := workflow.AwaitWithTimeout(ctx, time.Hour, func() bool { return done })
	//pathkit:ignore
	if !ok {
		return "timeout", nil
	}
	return "done", nil
}
