package model_test

import (
	"fmt"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func constFacts(t *testing.T, name string) []string {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/fixtures/constcond")["constcond."+name]
	if !ok {
		t.Fatalf("fixture constcond.%s not found", name)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out []string
	for _, j := range g.Junctions {
		for _, f := range j.Facts {
			if f.Kind == model.ConstCondition {
				out = append(out, fmt.Sprintf("%s:never %s", j.ID, f.Exit))
			}
		}
	}
	return out
}

func TestConstFacts(t *testing.T) {
	cases := map[string]string{ // "" = no fact
		"CatchIfFalse": "J1:never true", "CatchNotTrue": "J1:never true", "CatchArithmetic": "J1:never true",
		"CatchConstName": "J1:never true", "CatchConstNegated": "J1:never false", "CatchConstExpression": "J1:never false",
		"CatchLenOfArray": "J1:never true", "CatchIota": "J1:never false", "CatchStringConst": "J1:never true",
		"PackageVar": "", "LocalVar": "", "RuntimeGOOS": "", "IntSize": "", "MaxInt": "", "ShiftExpression": "",
		"XorExpression": "", "PlatformConst": "", "TaggedConst": "", "MixedWithVariable": "",
		"ConstantInSwitchTag": "", "ArrayLengthThroughType": "", "DotImportedUnsafe": "", "ConstantLoopCondition": "",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got := constFacts(t, name)
			if want == "" && len(got) != 0 || want != "" && (len(got) != 1 || got[0] != want) {
				t.Fatalf("got %v, want %q", got, want)
			}
		})
	}
}
