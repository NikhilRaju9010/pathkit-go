package classify

import "testing"

func TestVersionGateOldBranch(t *testing.T) {
	const sp = NeedsSpecialTest
	checkExpectations(t, "../../testdata/fixtures/versiongate", "versiongate.", []expectation{
		{"CatchEqDefault", "J1.true", sp, "version-gate-old-branch"},
		{"CatchNeDefault", "J1.false", sp, "version-gate-old-branch"},
		{"CatchLess", "J1.true", sp, "version-gate-old-branch"},
		{"CatchGreaterEq", "J1.false", sp, "version-gate-old-branch"},
		{"CatchFlipped", "J1.true", sp, "version-gate-old-branch"},
		{"CatchInline", "J1.true", sp, "version-gate-old-branch"},
		{"CatchSwitch", `J1.case workflow.DefaultVersion`, sp, "version-gate-old-branch"},
		{"PassedToHelper", "", "", ""},
		{"NotConstant", "", "", ""},
		{"JustAName", "", "", ""},
		{"RangeNotConstant", "", "", ""},
		{"OneSidedRange", "", "", ""},
		{"MiddleVersion", "", "", ""},
		{"Reassigned", "", "", ""},
		{"Compound", "", "", ""},
		{"SwitchLatestOnly", "", "", ""},
		{"NotTheSDK", "", "", ""},
		{"Incremented", "", "", ""},
		{"CatchBoolVar", "J1.false", sp, "version-gate-old-branch"},
		{"CatchBoolVarNegated", "J1.true", sp, "version-gate-old-branch"},
		{"CatchHelperGate", "J1.false", sp, "version-gate-old-branch"},
		{"NotAGateFunction", "", "", ""},
		{"GateFunctionWithMore", "", "", ""},
	})
}

// A switch with two old versions marks both old cases and not the rest.
func TestVersionGateTwoOldCases(t *testing.T) {
	old := 0
	for _, d := range detected(t, "../../testdata/fixtures/versiongate", "versiongate.CatchSwitchTwoOld") {
		if d.C.Label == "version-gate-old-branch" {
			old++
			if !takes(d.Path, "J1.case workflow.DefaultVersion") && !takes(d.Path, "J1.case 1") {
				t.Errorf("wrong path marked: %s", d.Path.Key())
			}
		} else if takes(d.Path, "J1.case workflow.DefaultVersion") || takes(d.Path, "J1.case 1") {
			t.Errorf("old case not marked: %s", d.Path.Key())
		}
	}
	if old != 2 {
		t.Errorf("%d old paths marked, want 2", old)
	}
}

func TestHistoryLimit(t *testing.T) {
	const sp, rev = NeedsSpecialTest, NeedsReview
	checkExpectations(t, "../../testdata/fixtures/historysuggest", "historysuggest.", []expectation{
		{"CatchDirect", "J1.true", sp, "history-limit"},
		{"CatchViaInfo", "J1.true", sp, "history-limit"},
		{"CatchViaBool", "J1.true", sp, "history-limit"},
		{"CatchNegated", "J1.false", sp, "history-limit"},
		{"WeakLength", "J1.true", rev, "needs-review"},
		{"WeakSizeReversed", "J1.false", rev, "needs-review"},
		{"OtherType", "", "", ""},
		{"PlainBool", "", "", ""},
		{"Compound", "", "", ""},
		{"LoopCondition", "", "", ""},
		{"LengthVsVariable", "", "", ""},
		{"Reassigned", "", "", ""},
	})
}
