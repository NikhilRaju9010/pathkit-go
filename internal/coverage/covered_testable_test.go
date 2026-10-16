package coverage

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// Whatever a classifier says, a path a test ran comes out testable. Here a
// hostile classifier calls every path cannot-be-tested and a trace runs every
// path of several fixtures; the check must pass, with a warning per path.
func TestCoveredPathsAreAlwaysTestable(t *testing.T) {
	hostile := func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction, c.Explanation = classify.CannotBeTested, "contradictory", classify.Certain, false, "hostile test claim"
		return c
	}
	for _, f := range []struct{ dir, name string }{
		{"../../testdata/fixtures/closedchannel", "closedchannel.CatchReceive"},
		{"../../testdata/fixtures/dataflow", "dataflow.TwiceLocal"},
		{"../../testdata/shapes/valueflow", "valueflow.ContradictionThroughAssign"},
		{"../../testdata/shapes/loopchange", "loopchange.ChangedInBody"},
	} {
		w := fixtureWorkflow(t, f.dir, f.name)
		var traces []TraceFile
		for i, p := range w.Graph.Paths(model.DefaultMaxPaths).List {
			var steps []string
			for _, s := range p.Steps {
				steps = append(steps, s.Exit.ID.String())
			}
			traces = append(traces, tf(string(rune('a'+i)), w, "complete", steps...))
		}
		res := Compute(Input{Workflows: []Workflow{w}, Traces: traces, Classifier: hostile})
		if err := CheckCoveredAreTestable(res); err != nil {
			t.Errorf("%s: %v", f.name, err)
		}
		if res.Covered != len(traces) || res.Breakdown.CannotBeTested.Paths != 0 {
			t.Errorf("%s: want all %d paths covered and testable, got covered=%d %+v", f.name, len(traces), res.Covered, res.Breakdown)
		}
		if len(res.Warnings) < len(traces) {
			t.Errorf("%s: want a warning for every overruled claim, got %d", f.name, len(res.Warnings))
		}
	}
}

// The check itself catches a covered path that is not testable.
func TestCheckCoveredAreTestableCatchesAViolation(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/closedchannel", "closedchannel.CatchReceive")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true")}, Classifier: e.Classify})
	if err := CheckCoveredAreTestable(res); err != nil {
		t.Fatalf("a normal run must pass: %v", err)
	}
	res.Workflows[0].Paths[0].Class.Bucket = classify.CannotBeTested // break it on purpose
	err = CheckCoveredAreTestable(res)
	if err == nil || !strings.Contains(err.Error(), "run by a test") {
		t.Errorf("want an error about a covered path that is not testable, got %v", err)
	}
}

// A possible-correlation suspect is only for paths no test ran. When a test
// ran the path, it is simply testable, with no warning (a real run settled
// it); an engine claim of impossible on a path a test ran still warns.
func TestCoveredSuspectIsQuietlyTestable(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/factsengine", "factsengine.SuspectLoopBetween")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddGraph(w.Name, w.Graph); err != nil {
		t.Fatal(err)
	}
	// Not run: the suspect is needs-review.
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.false", "J2.false")}, Classifier: e.Classify})
	if res.Breakdown.NeedsReview.Paths != 1 {
		t.Fatalf("want one needs-review suspect, got %+v", res.Breakdown)
	}
	// Run: testable, covered, nothing to warn about.
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true", "J2.true")}, Classifier: e.Classify})
	if res.Breakdown.NeedsReview.Paths != 0 || res.Covered != 1 || len(res.Warnings) != 0 {
		t.Errorf("a covered suspect: %+v warnings %v", res.Breakdown, res.Warnings)
	}
	// A claim of impossible on a path a test ran: testable, with a warning.
	z := fixtureWorkflow(t, "../../testdata/shapes/zerovalue", "zerovalue.NeverAssigned")
	if err := e.AddGraph(z.Name, z.Graph); err != nil {
		t.Fatal(err)
	}
	res = Compute(Input{Workflows: []Workflow{z}, Traces: []TraceFile{tf("b", z, "complete", "J1.false")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 0 || len(res.Warnings) != 1 || res.InternalError != nil {
		t.Errorf("a covered engine claim: %+v warnings %v err %v", res.Breakdown, res.Warnings, res.InternalError)
	}
}

// unknown-input-type is only for a case no test has taken: a path that was
// run, or whose case another run took, is an ordinary path.
func TestUnknownInputTypeNeedsAnUntakenCase(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/shapes/typeswitch", "typeswitch.SwitchWithUnknownValue")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddGraph(w.Name, w.Graph); err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.NeedsReview.Paths != 3 {
		t.Fatalf("no trace: want 3 needs-review paths, got %+v", res.Breakdown)
	}
	// A run of the "case string" path settles that path only.
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", `J1.case string`)}, Classifier: e.Classify})
	if res.Breakdown.NeedsReview.Paths != 2 || res.Covered != 1 || len(res.Warnings) != 0 {
		t.Errorf("one case run: %+v warnings %v", res.Breakdown, res.Warnings)
	}
}
