package coverage

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func claimEvery(bucket classify.Bucket, label string) classify.Classifier {
	return func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Confidence, c.Explanation = bucket, label, classify.Certain, "test claim"
		c.PossibleInProduction = bucket != classify.CannotBeTested
		return c
	}
}

func allSteps(w Workflow) [][]string {
	var out [][]string
	for _, p := range w.Graph.Paths(model.DefaultMaxPaths).List {
		var steps []string
		for _, s := range p.Steps {
			steps = append(steps, s.Exit.ID.String())
		}
		out = append(out, steps)
	}
	return out
}

func statuses(wr WorkflowResult) map[string]int {
	m := map[string]int{}
	for _, o := range wr.Outcomes {
		m[o.Status]++
	}
	return m
}

const branchFixture = "../../testdata/fixtures/dataflow"

// Plain run, no classifier: every outcome is possible; the ones a trace took
// are covered, the rest missed. Nothing leaves the total.
func TestOutcomesCoveredAndMissed(t *testing.T) {
	w := fixtureWorkflow(t, branchFixture, "dataflow.TwiceLocal")
	steps := allSteps(w)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", steps[0]...)}})
	wr := res.Workflows[0]
	st := statuses(wr)
	if st[OutcomeImpossible] != 0 || st[OutcomeBlocked] != 0 {
		t.Fatalf("a plain run removes nothing: %v", st)
	}
	if st[OutcomeCovered] == 0 || st[OutcomeMissed] == 0 {
		t.Fatalf("want some covered and some missed outcomes: %v", st)
	}
	if wr.BranchCov.Possible != len(wr.Outcomes) || wr.BranchCov.Covered != st[OutcomeCovered] {
		t.Errorf("summary %+v does not match %v", wr.BranchCov, st)
	}
	for _, o := range wr.Outcomes {
		if o.ID == "" || o.Decision == "" {
			t.Errorf("an outcome with no id or words: %+v", o)
		}
	}
}

// When every path is proven impossible and no test ran, every outcome can't
// happen and leaves the total; with one test run covering a path, the outcomes
// that path takes stay (a run proves them possible).
func TestOutcomesImpossibleLeaveTheTotal(t *testing.T) {
	w := fixtureWorkflow(t, branchFixture, "dataflow.TwiceLocal")
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: claimEvery(classify.CannotBeTested, "contradictory")})
	wr := res.Workflows[0]
	if wr.BranchCov.Possible != 0 || wr.BranchCov.Impossible != len(wr.Outcomes) || len(wr.Outcomes) == 0 {
		t.Fatalf("want every outcome impossible: %+v of %d", wr.BranchCov, len(wr.Outcomes))
	}
	for _, o := range wr.Outcomes {
		if o.Why == "" || o.Label != "contradictory" {
			t.Errorf("an impossible outcome needs its reason: %+v", o)
		}
	}
	// Near miss: a test took one path, so its outcomes are covered and possible.
	steps := allSteps(w)
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", steps[0]...)}, Classifier: claimEvery(classify.CannotBeTested, "contradictory")})
	wr = res.Workflows[0]
	if wr.BranchCov.Covered == 0 || wr.BranchCov.Possible < wr.BranchCov.Covered {
		t.Errorf("a covered outcome is always possible: %+v", wr.BranchCov)
	}
}

// A known bug keeps the outcome in the total and shows it apart.
func TestOutcomesBlockedByKnownBug(t *testing.T) {
	w := fixtureWorkflow(t, branchFixture, "dataflow.TwiceLocal")
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: claimEvery(classify.NeedsReview, classify.KnownDefect)})
	wr := res.Workflows[0]
	if wr.BranchCov.Blocked != len(wr.Outcomes) || wr.BranchCov.Possible != len(wr.Outcomes) || wr.BranchCov.Impossible != 0 {
		t.Fatalf("want every outcome blocked and still counted: %+v", wr.BranchCov)
	}
	// Near miss: needs-review without the known-defect label is just missed.
	res = Compute(Input{Workflows: []Workflow{w}, Classifier: claimEvery(classify.NeedsReview, "needs-review")})
	if res.Workflows[0].BranchCov.Blocked != 0 || res.Workflows[0].BranchCov.Impossible != 0 {
		t.Errorf("needs-review must stay in as missed: %+v", res.Workflows[0].BranchCov)
	}
}

// An old trace (recorded before condition parts were recorded) says nothing
// about them: the parts are "not recorded", never counted as missed-by-test.
// Near miss: a trace of the newer recorder with parts counts them.
func TestConditionPartsNotRecordedByOldTraces(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/conditions", "conditions.Chain")
	if len(w.Graph.Conditions()) != 4 {
		t.Fatalf("want 4 parts, got %d", len(w.Graph.Conditions()))
	}
	steps := allSteps(w)[0]
	old := tf("a", w, "complete", steps...)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{old}})
	if c := res.Workflows[0].Conditions; c.Total != 8 || c.Covered != 0 || !c.NotRecorded {
		t.Errorf("old trace: %+v", c)
	}
	fresh := tf("b", w, "complete", steps...)
	fresh.File.Recorder = 4
	first := w.Graph.Conditions()[0].ID
	fresh.File.Conditions = []string{first + ":t", first + ":f"}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{old, fresh}})
	if c := res.Workflows[0].Conditions; c.Covered != 2 || c.NotRecorded || !c.Parts[0].True || !c.Parts[0].False || c.Parts[1].True {
		t.Errorf("new trace: %+v", c)
	}
}

// A helper's decision is counted once, however many places call it: with
// the helper copied in, TwoCalls has two copies of charge's error check but
// one failure outcome and one success outcome for it, with two sites each.
func TestHelperOutcomesCountOncePerHelper(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/helpers", "helpers.TwoCalls")
	res := Compute(Input{Workflows: []Workflow{w}})
	wr := res.Workflows[0]
	n := 0
	for _, o := range wr.Outcomes {
		if o.Helper {
			n++
			if o.Sites != 2 {
				t.Errorf("%s %s: %d sites, want 2", o.Decision, o.Outcome, o.Sites)
			}
		}
	}
	if n != 2 {
		t.Errorf("want 2 helper outcomes (failure, success), got %d of %d", n, len(wr.Outcomes))
	}
	// Near miss: the workflow's own two error checks (different text) are not merged.
	if len(wr.Outcomes) != 6 {
		t.Errorf("want 6 outcomes (2 for the helper, 2 for each of the 2 own checks), got %d", len(wr.Outcomes))
	}
}

// An outcome a test ran is never "can't happen", even when that test's trace
// did not count (it fits no path): the outcome stays in the total as missed.
func TestOutcomeRanByAnUncountedTraceIsNotImpossible(t *testing.T) {
	w := fixtureWorkflow(t, branchFixture, "dataflow.TwiceLocal")
	first := allSteps(w)[0]
	bad := tf("x", w, "complete", append(append([]string{}, first...), "J99.nope")...)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad}, Classifier: claimEvery(classify.CannotBeTested, "contradictory")})
	wr := res.Workflows[0]
	if res.Counts.Unmatched != 1 {
		t.Fatalf("the trace should fit no path: %+v", res.Counts)
	}
	ran := map[string]bool{}
	for _, id := range first {
		ran[id] = true
	}
	n := 0
	for _, o := range wr.Outcomes {
		for _, e := range o.Exits {
			if ran[e] {
				n++
				if o.Status == OutcomeImpossible {
					t.Errorf("%s was run by a test but is called impossible", o.Exits)
				}
			}
		}
	}
	if n == 0 || wr.BranchCov.Impossible == len(wr.Outcomes) {
		t.Errorf("nothing was protected: %d outcomes run, %+v", n, wr.BranchCov)
	}
}

// A workflow with more paths than the list holds is not fully known: nothing
// in it may be called impossible, whatever a classifier says.
func TestCappedWorkflowRemovesNothing(t *testing.T) {
	w := fixtureWorkflow(t, "../../testdata/fixtures/bigflow", "bigflow.TwentyFourIfs")
	res := Compute(Input{Workflows: []Workflow{w}, Classifier: claimEvery(classify.CannotBeTested, "contradictory")})
	wr := res.Workflows[0]
	if !wr.Capped {
		t.Skip("the fixture is not over the cap any more")
	}
	if wr.BranchCov.Impossible != 0 || wr.BranchCov.Possible != len(wr.Outcomes) || len(wr.Outcomes) == 0 {
		t.Errorf("a capped workflow keeps every outcome: %+v of %d", wr.BranchCov, len(wr.Outcomes))
	}
}
