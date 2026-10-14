package classify

import (
	"strings"
	"testing"
)

const constFixtures = "../../testdata/fixtures/constcond"

func TestConstantCondition(t *testing.T) {
	const cant = CannotBeTested
	checkExpectations(t, constFixtures, "constcond.", []expectation{
		// catch: the side that can never run is cannot-be-tested
		{"CatchIfFalse", "J1.true", cant, "constant-condition"},
		{"CatchNotTrue", "J1.true", cant, "constant-condition"},
		{"CatchArithmetic", "J1.true", cant, "constant-condition"},
		{"CatchConstName", "J1.true", cant, "constant-condition"},
		{"CatchConstNegated", "J1.false", cant, "constant-condition"},
		{"CatchConstExpression", "J1.false", cant, "constant-condition"},
		{"CatchLenOfArray", "J1.true", cant, "constant-condition"},
		{"CatchIota", "J1.false", cant, "constant-condition"},
		{"CatchStringConst", "J1.true", cant, "constant-condition"},
		// near-miss: looks constant, is not (or differs by machine or build)
		{"PackageVar", "", "", ""},
		{"LocalVar", "", "", ""},
		{"RuntimeGOOS", "", "", ""},
		{"IntSize", "", "", ""},
		{"MaxInt", "", "", ""},
		{"ShiftExpression", "", "", ""},
		{"XorExpression", "", "", ""},
		{"PlatformConst", "", "", ""},
		{"TaggedConst", "", "", ""},
		{"MixedWithVariable", "", "", ""},
		{"ConstantInSwitchTag", "", "", ""},
		{"ConstantLoopCondition", "", "", ""},
		{"ArrayLengthThroughType", "", "", ""},
		{"DotImportedUnsafe", "", "", ""},
	})
}

func TestConstantConditionExplanation(t *testing.T) {
	for _, d := range detected(t, constFixtures, "constcond.CatchConstName") {
		if d.C.Label != "constant-condition" {
			continue
		}
		for _, want := range []string{"J1", "if debug", "true side", "always false"} {
			if !strings.Contains(d.C.Explanation, want) {
				t.Errorf("explanation %q lacks %q", d.C.Explanation, want)
			}
		}
		return
	}
	t.Fatal("no path was marked")
}
