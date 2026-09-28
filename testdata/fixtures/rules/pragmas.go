package rules

import "go.temporal.io/sdk/workflow"

func IgnoredErrCheck(ctx workflow.Context) (string, error) {
	ctx = withOptions(ctx)
	err := workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
	//pathkit:ignore
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func IgnoredPlainIf(ctx workflow.Context, x int) (string, error) {
	if x > 1000 { //pathkit:ignore
		return "huge", nil
	}
	return "normal", nil
}

func ForcedBranch(ctx workflow.Context, s string) (string, error) {
	_, err := decode(s)
	//pathkit:branch
	if err != nil {
		return "", err
	}
	return "ok", nil
}
