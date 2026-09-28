package rules

import (
	"errors"
	"go/types"

	"go.temporal.io/sdk/workflow"
)

// Every switch with at least one case is a junction (CLAUDE.md D3): one
// exit per case in source order, plus "default", written or not.

func UsesSwitch(ctx workflow.Context, s string) (string, error) {
	switch s {
	case "a":
		return "A", nil
	}
	return "other", nil
}

func UsesTypeSwitch(ctx workflow.Context, v any) (string, error) {
	switch v.(type) {
	case int:
		return "int", nil
	}
	return "other", nil
}

// A written default can sit anywhere; exits keep source order.
func SwitchWithDefault(ctx workflow.Context, s string) (string, error) {
	switch s {
	case "a", "b":
		return "AB", nil
	default:
		return "", errors.New("unknown")
	case "c":
		s = "C"
	}
	return s, nil
}

func TaglessSwitchWithInit(ctx workflow.Context, n int) (string, error) {
	switch m := n * 2; {
	case m > 10:
		return "big", nil
	case m > 4:
		if n == 3 {
			return "three", nil
		}
	}
	return "small", nil
}

func TypeSwitchAssign(ctx workflow.Context, v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "nil", nil
	case string, int:
		_ = x
		return "scalar", nil
	default:
		return "", errors.New("odd")
	}
}

// Falling into the next case is not a new decision: no extra exit.
func Fallthrough(ctx workflow.Context, n int) (string, error) {
	out := ""
	switch {
	case n > 10:
		out += "big "
		fallthrough
	case n > 5:
		out += "medium"
	default:
		out = "small"
	}
	return out, nil
}

// A switch with only a default has one outcome: not a junction.
func OnlyDefault(ctx workflow.Context, n int) (string, error) {
	switch n {
	default:
		return "any", nil
	}
}

// break leaves the switch, not the workflow.
func BreakInSwitch(ctx workflow.Context, s string) (string, error) {
	switch s {
	case "skip":
		if len(s) > 100 {
			break
		}
		return "skipped", nil
	}
	return "done", nil
}

func SwitchOnActivityResult(ctx workflow.Context, x int) (string, error) {
	ctx = withOptions(ctx)
	var status string
	err := workflow.ExecuteActivity(ctx, Charge, x).Get(ctx, &status)
	if err != nil {
		return "", err
	}
	switch status {
	case "ok":
		return "paid", nil
	case "retry":
		return "", errors.New("retry later")
	}
	return "unknown", nil
}

// Two cases with the same text get distinct exit IDs.
func DuplicateCases(ctx workflow.Context, x int) (string, error) {
	switch {
	case x > 0:
		return "a", nil
	case x > 0:
		return "b", nil
	}
	return "c", nil
}

// A qualified type in a case prints as written.
func QualifiedTypeCase(ctx workflow.Context, t types.Type) (string, error) {
	switch t.(type) {
	case *types.Basic:
		return "basic", nil
	}
	return "other", nil
}
