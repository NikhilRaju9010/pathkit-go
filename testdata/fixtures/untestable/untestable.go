// Package untestable holds workflows for the //pathkit:untestable comment:
// one that works, and one for each way a comment can be wrong.
package untestable

import "go.temporal.io/sdk/workflow"

// Checkout has two decisions; the first has a valid comment.
func Checkout(ctx workflow.Context, express bool, amount int) (string, error) {
	//pathkit:untestable exit="true" reason="express is never combined with a zero amount"
	if express && amount == 0 {
		return "free-express", nil
	}
	if amount > 100 {
		return "large", nil
	}
	return "ok", nil
}

// SameLine has the comment on the decision's own line.
func SameLine(ctx workflow.Context, n int) (string, error) {
	if n < 0 { //pathkit:untestable exit="true" reason="n is a count"
		return "negative", nil
	}
	return "ok", nil
}

// Switched marks one case of a switch.
func Switched(ctx workflow.Context, kind string) (string, error) {
	//pathkit:untestable exit="case \"legacy\"" reason="legacy orders are gone"
	switch kind {
	case "legacy":
		return "old", nil
	case "new":
		return "new", nil
	}
	return "other", nil
}

// TwoMarks has a comment on each of two decisions.
func TwoMarks(ctx workflow.Context, a, b bool) (string, error) {
	//pathkit:untestable exit="true" reason="first reason"
	if a {
		return "a", nil
	}
	//pathkit:untestable exit="false" reason="second reason"
	if b {
		return "b", nil
	}
	return "none", nil
}

// NotAComment has a comment of another kind, which PathKit leaves alone.
func NotAComment(ctx workflow.Context, a bool) (string, error) {
	//pathkit:other exit="true" reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}
