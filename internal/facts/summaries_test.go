package facts

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

const summariesDir = "../../testdata/fixtures/summaries/..."

// Claims the engine makes by reading the plain functions the workflows call
// (E2). Every Impossible... workflow has exactly these; every Possible... one
// has none; the LikelyDefect... one has a defect and no claim.
var wantSummaryClaims = map[string]map[string]Kind{
	"ImpossibleListOfMaps":        {"J1.case []interface{}|completed": ImpossibleType},
	"ImpossibleNameIsString":      {"J1.case int|completed": ImpossibleType},
	"ImpossibleMissingKey":        {"J1.case string|completed": ImpossibleType},
	"ImpossiblePayloadType":       {"J1.case float64|completed": ImpossibleType, "J1.default|completed": ImpossibleType},
	"ImpossibleTupleResult":       {"J1.case int|completed": ImpossibleType},
	"ImpossibleConstantResult":    {"J1.true|completed": ConstantCondition},
	"ImpossibleNeverNil":          {"J1.true|completed": ConstantCondition},
	"ImpossibleTwoLevels":         {"J1.case int|completed": ImpossibleType},
	"ImpossibleThroughVariable":   {"J1.case string|completed": ImpossibleType},
	"ImpossibleStoreAfterCall":    {"J1.case string|completed": ImpossibleType, "J1.default|completed": ImpossibleType},
	"ImpossibleOtherKeyKept":      {"J1.case []interface{}|completed": ImpossibleType},
	"ImpossibleClosureWriter":     {"J1.case int|completed": ImpossibleType},
	"ImpossibleClosureWriterKids": {"J1.case []interface{}|completed": ImpossibleType},
}

var wantSummaryDefects = map[string][]string{
	"LikelyDefectNoteIsPointer": {"J1.case string|completed"},
}

func TestSummariesOfPlainFunctions(t *testing.T) {
	all := analyzeFixture(t, summariesDir)
	if len(all) < 20 {
		t.Fatalf("only %d workflows found", len(all))
	}
	for full, f := range all {
		name := strings.TrimPrefix(full, "summaries.")
		t.Run(name, func(t *testing.T) {
			want := wantSummaryClaims[name]
			for key, kind := range f.claims {
				if got, ok := want[key]; !ok {
					t.Errorf("unexpected claim %s: %s (a false \"impossible\"? read the called function)", key, kind)
				} else if got != kind {
					t.Errorf("claim %s: %s, want %s", key, kind, got)
				}
			}
			for key, kind := range want {
				if _, ok := f.claims[key]; !ok {
					t.Errorf("missing claim %s: %s", key, kind)
				}
			}
			if strings.HasPrefix(name, "Possible") && len(f.claims) > 0 {
				t.Errorf("a Possible... workflow must make no claim")
			}
			sort.Strings(f.defects)
			if fmt.Sprint(f.defects) != fmt.Sprint(wantSummaryDefects[name]) && !(len(f.defects) == 0 && len(wantSummaryDefects[name]) == 0) {
				t.Errorf("likely defects %v, want %v", f.defects, wantSummaryDefects[name])
			}
		})
	}
}

// A claim that rests on a summary says where the called function is.
func TestSummaryProofNamesTheFunction(t *testing.T) {
	f := analyzeFixture(t, summariesDir)["summaries.ImpossibleListOfMaps"]
	proof := f.proofs["J1.case []interface{}|completed"]
	text := ""
	for _, s := range proof {
		text += s.Text + "\n"
	}
	if !strings.Contains(text, "ToMap") || !strings.Contains(text, "Children") || !strings.Contains(text, "[]map[string]interface{}") {
		t.Errorf("the proof should name the function, the key and the type:\n%s", text)
	}
}
