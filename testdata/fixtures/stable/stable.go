// Package stable holds workflows that differ by one added junction, to
// prove that a path's stableId does not change when an unrelated junction
// is added or removed (while its older, J-number based id does).
package stable

import "go.temporal.io/sdk/workflow"

// Before has three plain ifs in a row, J1, J2 and J3.
func Before(ctx workflow.Context, a, b, c bool) (string, error) {
	if a {
		return "a", nil
	}
	if b {
		return "b", nil
	}
	if c {
		return "c", nil
	}
	return "none", nil
}

// After is Before with one junction added inside the first branch. The
// junctions after it are renumbered (b becomes J3, c becomes J4). Every
// path that does not go through the new `if extra` is unrelated to the
// change: "a is false" paths, which is all of them except the first branch.
func After(ctx workflow.Context, a, b, c, extra bool) (string, error) {
	if a {
		if extra {
			return "a-extra", nil
		}
		return "a", nil
	}
	if b {
		return "b", nil
	}
	if c {
		return "c", nil
	}
	return "none", nil
}

// SameText has two ifs with exactly the same text.
func SameText(ctx workflow.Context, a bool) (string, error) {
	if a {
		workflow.GetLogger(ctx).Info("first")
	}
	if a {
		return "second", nil
	}
	return "none", nil
}

// OneLineCase and MultiLineCase are the same switch; only the line
// breaks inside the case list differ, so their stableIds must be equal.
func OneLineCase(ctx workflow.Context, s string) (string, error) {
	switch s {
	case "a", "b":
		return "ab", nil
	}
	return "none", nil
}

func MultiLineCase(ctx workflow.Context, s string) (string, error) {
	switch s {
	case "a",
		"b":
		return "ab", nil
	}
	return "none", nil
}
