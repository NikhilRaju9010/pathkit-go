// Package untestablebad holds workflows whose //pathkit:untestable comment
// is wrong in some way. Each one must fail to build with a clear message.
package untestablebad

import "go.temporal.io/sdk/workflow"

// StackedMarks has two comments in a row: the first is two lines above the
// decision, so it has nothing to mark.
func StackedMarks(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="true" reason="first reason"
	//pathkit:untestable exit="false" reason="second reason"
	if a {
		return "a", nil
	}
	return "none", nil
}

func NoReason(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="true"
	if a {
		return "a", nil
	}
	return "none", nil
}

func BlankReason(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="true" reason=" "
	if a {
		return "a", nil
	}
	return "none", nil
}

func NoExit(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}

func UnknownExit(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="maybe" reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}

func UnknownSetting(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="true" reason="because" colour="red"
	if a {
		return "a", nil
	}
	return "none", nil
}

func Unquoted(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit=true reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}

// NothingToMark has the comment above a statement that is not a decision.
func NothingToMark(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestable exit="true" reason="because"
	x := 1
	_ = x
	if a {
		return "a", nil
	}
	return "none", nil
}

// OnTransparent has the comment on an error check PathKit does not map.
func OnTransparent(ctx workflow.Context, a string) (string, error) {
	//pathkit:untestable exit="true" reason="because"
	if err := validate(a); err != nil {
		return "", err
	}
	return "none", nil
}

func validate(string) error { return nil }

func NearMissSpace(ctx workflow.Context, a bool) (string, error) {
	// pathkit:untestable exit="true" reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}

func NearMissTypo(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestible exit="true" reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}

func NearMissPlural(ctx workflow.Context, a bool) (string, error) {
	//pathkit:untestables exit="true" reason="because"
	if a {
		return "a", nil
	}
	return "none", nil
}
