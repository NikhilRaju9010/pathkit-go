package model

import "testing"

// StableKey collapses whitespace in an exit label. The model prints case
// lists on one line today, so this builds the odd label by hand: without
// the collapse the two IDs differ.
func TestStableKeyCollapsesWhitespaceInExitLabel(t *testing.T) {
	j := &Junction{stable: "switch s"}
	one := Path{Steps: []Step{{Junction: j, Exit: &Exit{Label: `case "a", "b"`}}}, End: EndCompleted}
	many := Path{Steps: []Step{{Junction: j, Exit: &Exit{Label: "case \"a\",\n\t\t\"b\""}}}, End: EndCompleted}
	if one.StableID() != many.StableID() {
		t.Errorf("line breaks and tabs in a label changed the stableId: %s vs %s\n%q\n%q", one.StableID(), many.StableID(), one.StableKey(), many.StableKey())
	}
}
