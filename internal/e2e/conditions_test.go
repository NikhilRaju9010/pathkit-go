package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// "Each part of a condition": the recorder notes every part of an && / || chain
// as it is evaluated (the parts after the deciding one are skipped), and the
// report counts them. The numbers below are the fixture's own tests.
func TestConditionPartsAreRecordedAndCounted(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/conditions")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", root, "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	if len(files) != 8 {
		t.Fatalf("%d traces, want 8", len(files))
	}
	out, errOut, code := pathkit(t, "report", root, "--traces", traces, "--json")
	if code != 0 {
		t.Fatalf("report: exit %d\n%s", code, errOut)
	}
	var r struct {
		ConditionCoverage *struct{ Covered, Total int } `json:"conditionCoverage"`
		Workflows         []struct {
			Name              string                        `json:"name"`
			ConditionCoverage *struct{ Covered, Total int } `json:"conditionCoverage"`
			Conditions        []struct {
				Part  string `json:"part"`
				True  bool   `json:"seenTrue"`
				False bool   `json:"seenFalse"`
			} `json:"conditions"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"conditions.Chain": {7, 8}, "conditions.Named": {2, 4}, "conditions.HelperAnd": {3, 4}, "conditions.Repeated": {1, 4}}
	seen := 0
	for _, w := range r.Workflows {
		switch w.Name {
		case "conditions.Single":
			if w.ConditionCoverage != nil {
				t.Errorf("a one-part condition has no parts to count: %+v", w.ConditionCoverage)
			}
		case "conditions.Chain":
			// the run with Amount 0 never evaluates the other parts
			for _, c := range w.Conditions {
				if c.Part == "req.Express" && (!c.True || c.False) {
					t.Errorf("req.Express: want seen true only, got %+v", c)
				}
			}
		}
		if exp, ok := want[w.Name]; ok {
			seen++
			if w.ConditionCoverage == nil || w.ConditionCoverage.Covered != exp[0] || w.ConditionCoverage.Total != exp[1] {
				t.Errorf("%s: condition coverage %+v, want %d/%d", w.Name, w.ConditionCoverage, exp[0], exp[1])
			}
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d expected workflows", seen, len(want))
	}
	if r.ConditionCoverage == nil || r.ConditionCoverage.Total != 28 || r.ConditionCoverage.Covered != 17 {
		t.Errorf("project condition coverage %+v, want 17/28", r.ConditionCoverage)
	}
}
