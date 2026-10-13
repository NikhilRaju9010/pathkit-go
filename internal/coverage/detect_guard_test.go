package coverage

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

func fixtureWorkflow(t *testing.T, folder, name string) Workflow {
	t.Helper()
	res, err := load.Load(folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name == name {
			g, err := model.Build(wf)
			if err != nil {
				t.Fatal(err)
			}
			return Workflow{Name: name, Graph: g, Hash: model.FunctionHash(wf.Pkg.Fset, wf.Func)}
		}
	}
	t.Fatalf("%s not found", name)
	return Workflow{}
}

// The detector says the closed side of a signal channel is impossible. If a
// trace nevertheless took it, the path was clearly possible: it stays
// testable, is counted covered, and the report warns. A detector can never
// hide a path a test ran.
func TestDetectorNeverHidesACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/closedchannel", "closedchannel.CatchReceive")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Without a trace: the closed path is cannot-be-tested.
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 1 || res.Breakdown.Testable.Paths != 1 {
		t.Fatalf("want 1 cannot-be-tested and 1 testable path, got %+v", res.Breakdown)
	}
	// A trace took J1.true (the closed side).
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 0 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
	warned := false
	for _, x := range res.Warnings {
		warned = warned || x.Kind == "classification"
	}
	if !warned {
		t.Error("no warning for a detector that was wrong")
	}
}

// The data-flow detectors obey the same guard: if a trace took two
// "contradictory" exits, the path was possible and stays testable.
func TestDataflowDetectorsNeverHideACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/dataflow", "dataflow.TwiceLocal")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 2 {
		t.Fatalf("want 2 cannot-be-tested paths, got %+v", res.Breakdown)
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true", "J2.true")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 1 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
	warned := false
	for _, x := range res.Warnings {
		warned = warned || x.Kind == "classification"
	}
	if !warned {
		t.Error("no warning for a detector that was wrong")
	}
}

// The list-built-from-list detector obeys the same guard: a trace that
// took "first loop skipped, second loop ran" proves the path possible.
func TestListBuiltDetectorNeverHidesACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/builtlist", "builtlist.BuiltTwoLoops")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 2 {
		t.Fatalf("want 2 cannot-be-tested paths, got %+v", res.Breakdown)
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.exit", "J2.iterate", "J2.retry", "J2.exit")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 1 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
	warned := false
	for _, x := range res.Warnings {
		warned = warned || x.Kind == "classification"
	}
	if !warned {
		t.Error("no warning for a detector that was wrong")
	}
}

// The loop-exit detector obeys the same guard.
func TestLoopFirstTripDetectorNeverHidesACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/loopfirst", "loopfirst.FlagLoop")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 1 {
		t.Fatalf("want 1 cannot-be-tested path, got %+v", res.Breakdown)
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.exit")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 0 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
	warned := false
	for _, x := range res.Warnings {
		warned = warned || x.Kind == "classification"
	}
	if !warned {
		t.Error("no warning for a detector that was wrong")
	}
}

// The Get-result detector obeys the same guard.
func TestGetResultDetectorNeverHidesACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/getvalue", "getvalue.ValueTwice")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 2 {
		t.Fatalf("want 2 cannot-be-tested paths, got %+v", res.Breakdown)
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true", "J2.true")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 1 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
	warned := false
	for _, x := range res.Warnings {
		warned = warned || x.Kind == "classification"
	}
	if !warned {
		t.Error("no warning for a detector that was wrong")
	}
}

// The same guard for the "declared first, assigned once" signal channel.
func TestLateAssignedChannelNeverHidesACoveredPath(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/closedchannel", "closedchannel.CatchLateAssigned")
	e, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: e.Classify})
	if res.Breakdown.CannotBeTested.Paths != 1 {
		t.Fatalf("want 1 cannot-be-tested path, got %+v", res.Breakdown)
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true")}, Classifier: e.Classify})
	if res.Covered != 1 || res.Breakdown.CannotBeTested.Paths != 0 || res.Breakdown.Testable.Covered != 1 {
		t.Fatalf("a covered path must be testable: %+v", res.Breakdown)
	}
}
