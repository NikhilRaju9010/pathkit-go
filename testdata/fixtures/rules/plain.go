package rules

import "go.temporal.io/sdk/workflow"

func PlainIfElse(ctx workflow.Context, x int) (string, error) {
	if x > 0 {
		return "positive", nil
	} else {
		return "other", nil
	}
}

func IfNoElse(ctx workflow.Context, x int) (string, error) {
	if x > 0 {
		return "positive", nil
	}
	return "other", nil
}

func ElseIfChain(ctx workflow.Context, x int) (string, error) {
	if x > 10 {
		return "big", nil
	} else if x > 0 {
		return "small", nil
	} else {
		return "none", nil
	}
}

func IfWithInit(ctx workflow.Context, s string) (string, error) {
	if n := len(s); n > 3 {
		return "long", nil
	}
	return "short", nil
}

func Sequential(ctx workflow.Context, a, b bool) (string, error) {
	out := ""
	if a {
		out += "a"
	}
	if b {
		out += "b"
	}
	return out, nil
}

func CompoundCondition(ctx workflow.Context, x int) (string, error) {
	ctx = withOptions(ctx)
	err := workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
	// Not a pure error check (it has "&& x > 0"), so it's a plain if.
	if err != nil && x > 0 {
		return "", err
	}
	return "done", nil
}

func Panics(ctx workflow.Context, x int) (string, error) {
	if x < 0 {
		panic("negative")
	}
	return "ok", nil
}

func NoBranches(ctx workflow.Context) (string, error) {
	return "ok", nil
}
