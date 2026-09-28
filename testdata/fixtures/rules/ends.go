package rules

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// EndKinds has one return of every kind, chosen by a plain if chain.
func EndKinds(ctx workflow.Context, n int) error {
	if n == 1 {
		return nil
	}
	if n == 2 {
		return workflow.NewContinueAsNewError(ctx, EndKinds, n)
	}
	if n == 3 {
		return fmt.Errorf("bad n %d", n)
	}
	if n == 4 {
		return errors.New("four")
	}
	if n == 5 {
		return temporal.NewApplicationError("five", "Five")
	}
	return helperErr()
}

func helperErr() error { return nil }

// NamedBare returns with a bare "return": PathKit can't tell the kind.
func NamedBare(ctx workflow.Context) (result string, err error) {
	result = "ok"
	return
}

// UncheckedVar returns an error variable nobody checked.
func UncheckedVar(ctx workflow.Context) error {
	ctx = withOptions(ctx)
	err := workflow.ExecuteActivity(ctx, Notify).Get(ctx, nil)
	return err
}
