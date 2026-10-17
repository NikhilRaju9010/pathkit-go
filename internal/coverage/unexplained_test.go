package coverage

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// claimAll says every path is impossible, once by a detector, and for the
// path whose stable id is manualID once by a person's override.
func claimAll(manualID string) classify.Classifier {
	return func(_ string, p model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Source, c.PossibleInProduction = classify.CannotBeTested, "contradictory", classify.SourceAuto, false
		c.Detector, c.Explanation = "contradictory", "the same value is tested twice."
		if p.StableID() == manualID {
			c.Source, c.Origin, c.Detector = classify.SourceManual, classify.OriginOverride, ""
			c.Explanation = "Marked untestable in the config (manual): never runs."
		}
		return c
	}
}

// A trace that fits no path (a decision the map does not know) puts every
// detector and rule claim of that workflow under review; a person's own
// mark is kept.
func TestUnexplainedTraceDowngradesClaims(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	bad := tf("bad", w, "complete", "J1.true", "J9.nowhere")
	paths := w.Graph.Paths(model.DefaultMaxPaths).List
	manual := paths[len(paths)-1].StableID()

	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad}, Classifier: claimAll(manual)})
	wf := res.Workflows[0]
	if len(wf.Unexplained) != 1 || wf.Unexplained[0].Count != 1 || wf.Unexplained[0].Acknowledged {
		t.Fatalf("unexplained = %+v", wf.Unexplained)
	}
	id := StepsID([]string{"J1.true", "J9.nowhere"})
	if wf.Unexplained[0].StepsID != id || !strings.HasPrefix(id, "t1:") || len(id) != 15 {
		t.Errorf("steps id %q", wf.Unexplained[0].StepsID)
	}
	for _, p := range wf.Paths {
		switch {
		case p.StableID == manual:
			if p.Class.Bucket != classify.CannotBeTested || p.Class.Origin != classify.OriginOverride {
				t.Errorf("a person's mark must be kept: %+v", p.Class)
			}
		default:
			if p.Class.Bucket != classify.NeedsReview || p.Class.Label != "model-limit" || p.Class.PossibleInProduction != true ||
				!strings.Contains(p.Class.Explanation, "detector contradictory would mark this path cannot-be-tested") ||
				!strings.Contains(p.Class.Explanation, id) {
				t.Errorf("claim not downgraded: %+v", p.Class)
			}
		}
	}
	var warned bool
	for _, x := range res.Warnings {
		if x.Kind == "classification" && strings.Contains(x.Message, id) && strings.Contains(x.Message, "1 path marked by a person is kept") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no warning naming the trace and the kept manual mark: %+v", res.Warnings)
	}
	// Needs-review counts as testable under the default policy: the
	// number cannot go up because of the downgrade.
	b := res.Breakdown
	if b.CannotBeTested.Paths != 1 || b.NeedsReview.Paths != len(paths)-1 {
		t.Errorf("breakdown %+v", b)
	}
	if got := b.TestableCoverage(); got.Paths != len(paths)-1 {
		t.Errorf("testable paths %d, want every path but the manual one", got.Paths)
	}
}

// Accepting the trace (with its steps id) gives the claims back; the report
// still lists the trace, as acknowledged.
func TestExplainedTraceRestoresClaims(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	bad := tf("bad", w, "complete", "J1.true", "J9.nowhere")
	id := StepsID([]string{"J1.true", "J9.nowhere"})
	in := Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad, bad}, Classifier: claimAll(""),
		Explained: []scope.ExplainedTrace{{Workflow: w.Name, StepsID: id, Reason: "returns early from a helper"}}}
	res := Compute(in)
	wf := res.Workflows[0]
	if len(wf.Unexplained) != 1 || wf.Unexplained[0].Count != 2 || !wf.Unexplained[0].Acknowledged || wf.Unexplained[0].Reason != "returns early from a helper" {
		t.Fatalf("unexplained = %+v", wf.Unexplained)
	}
	if res.Breakdown.NeedsReview.Paths != 0 || res.Breakdown.CannotBeTested.Paths != len(wf.Paths) {
		t.Errorf("claims not restored: %+v", res.Breakdown)
	}
	for _, x := range res.Warnings {
		if x.Kind == "classification" {
			t.Errorf("an acknowledged trace must not warn: %s", x.Message)
		}
	}
}

// An acknowledgement that matches no such trace is reported as stale; one
// for another workflow that is not measured is left alone.
func TestStaleExplanationWarns(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	in := Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("a", w, "complete", "J1.true")}, Classifier: claimAll(""),
		Explained: []scope.ExplainedTrace{
			{Workflow: w.Name, StepsID: "t1:0123456789ab", Reason: "old"},
			{Workflow: "other.Workflow", StepsID: "t1:0123456789ab", Reason: "not measured"},
		}}
	res := Compute(in)
	var stale int
	for _, x := range res.Warnings {
		if strings.Contains(x.Message, "stale entry: explained_traces") {
			stale++
			if !strings.Contains(x.Message, w.Name) {
				t.Errorf("wrong entry reported: %s", x.Message)
			}
		}
	}
	if stale != 1 {
		t.Errorf("%d stale warnings, want 1: %+v", stale, res.Warnings)
	}
}

// Without classification (no classifier) nothing is downgraded or warned,
// so existing output does not change.
func TestUnexplainedIgnoredWithoutClassification(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("bad", w, "complete", "J1.true", "J9.nowhere")}})
	for _, x := range res.Warnings {
		if x.Kind == "classification" {
			t.Errorf("unexpected: %s", x.Message)
		}
	}
	if res.Breakdown.NeedsReview.Paths != 0 {
		t.Errorf("%+v", res.Breakdown)
	}
}

// A trace that names another package is not matched to a workflow of the
// same name; an old trace without the field still is.
func TestTracePackageField(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	w.Package = "example.com/pilot/orders"
	old := tf("old", w, "complete", "J1.true")
	same := tf("same", w, "complete", "J1.true")
	same.File.Package = w.Package
	other := tf("other", w, "complete", "J1.true")
	other.File.Package = "example.com/elsewhere/orders"
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{old, same, other}})
	if res.Counts.Counted != 2 || res.Counts.Unknown != 1 {
		t.Errorf("counts %+v", res.Counts)
	}
	var msg string
	for _, x := range res.Warnings {
		if x.Kind == trace.UnknownWorkflow {
			msg = x.Message
		}
	}
	if !strings.Contains(msg, "example.com/elsewhere/orders") || !strings.Contains(msg, "example.com/pilot/orders") {
		t.Errorf("message should name both packages: %q", msg)
	}
	// no package known on the workflow side: nothing to compare, as before
	w.Package = ""
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{old, same, other}})
	if res.Counts.Counted != 3 {
		t.Errorf("counts %+v", res.Counts)
	}
}

// A //pathkit:untestable comment is a person's decision: it survives an
// unexplained trace, like an override does.
func TestCommentMarkSurvivesUnexplainedTrace(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	cls := func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Source, c.Origin, c.PossibleInProduction = classify.CannotBeTested, "manual", classify.SourceManual, classify.OriginComment, false
		return c
	}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("bad", w, "complete", "J1.true", "J9.nowhere")}, Classifier: cls})
	if res.Breakdown.NeedsReview.Paths != 0 || res.Breakdown.CannotBeTested.Paths != len(res.Workflows[0].Paths) {
		t.Errorf("comment marks were downgraded: %+v", res.Breakdown)
	}
}

// Under unclassified_policy "exclude" an untrusted claim must not raise
// testable coverage: the downgraded paths still count as testable.
func TestUnexplainedCountsUnderExcludePolicy(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("bad", w, "complete", "J1.true", "J9.nowhere")},
		Classifier: claimAll(""), Policy: classify.Exclude})
	n := len(res.Workflows[0].Paths)
	if got := res.Breakdown.TestableCoverage(); got.Paths != n {
		t.Errorf("testable paths %d, want %d (every path, the claims are not trusted)", got.Paths, n)
	}
}

// With classification off, the JSON-facing list stays empty.
func TestUnexplainedListEmptyWithoutClassification(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("bad", w, "complete", "J1.true", "J9.nowhere")}})
	if len(res.Workflows[0].Unexplained) != 0 {
		t.Errorf("%+v", res.Workflows[0].Unexplained)
	}
}

// A trace that fits the map for its first steps and then leaves it puts only
// the claims that rest on the step it leaves at, or a later one, under review.
// A claim whose reasoning stays before that step keeps its label.
func TestLocalDistrust(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	// The trace follows J1.false, then does something the map does not draw.
	bad := tf("bad", w, "complete", "J1.false", "J9.nowhere")
	usesLast := func(_ string, p model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Source, c.PossibleInProduction = classify.CannotBeTested, "value-flow", classify.SourceAuto, false
		c.Detector, c.Explanation = "value-flow", "the last step is impossible."
		c.UsesStep = len(p.Steps) // the claim rests on the whole path
		return c
	}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad}, Classifier: usesLast})
	wf := res.Workflows[0]
	if len(wf.Unexplained) != 1 || wf.Unexplained[0].divergence() != 2 {
		t.Fatalf("unexplained = %+v, divergence %d, want the trace to leave the map at step 2", wf.Unexplained, wf.Unexplained[0].divergence())
	}
	for _, p := range wf.Paths {
		steps := len(p.Path.Steps)
		switch {
		case steps == 1 && p.Class.Bucket != classify.CannotBeTested:
			t.Errorf("a claim resting on step 1 only must keep its label: %+v", p.Class)
		case steps == 2 && (p.Class.Bucket != classify.NeedsReview || p.Class.Label != "model-limit" || !strings.Contains(p.Class.Explanation, "rests on step 2")):
			t.Errorf("a claim resting on step 2 must go under review, naming the step: %+v", p.Class)
		}
	}

	// A claim whose steps are not known is not trusted, as before.
	unknown := func(w string, p model.Path) classify.Classification {
		c := usesLast(w, p)
		c.UsesStep = 0
		return c
	}
	res = Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad}, Classifier: unknown})
	for _, p := range res.Workflows[0].Paths {
		if p.Class.Bucket != classify.NeedsReview {
			t.Errorf("a claim with no known step must go under review: %+v", p.Class)
		}
	}
}

// The unmatched-trace warning carries the ready-made explained_traces entry,
// with the reason "TODO", for the trace.
func TestUnmatchedWarningHasReadyMadeEntry(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tf("bad", w, "complete", "J1.false", "J9.nowhere")}})
	id := StepsID([]string{"J1.false", "J9.nowhere"})
	found := false
	for _, x := range res.Warnings {
		if x.Kind == trace.Unmatched {
			found = true
			want := `{"workflow": "orders.OrderWorkflow", "steps_id": "` + id + `", "reason": "TODO"}`
			if x.Entry != want {
				t.Errorf("entry %q, want %q", x.Entry, want)
			}
			if strings.Contains(x.Message, "TODO") {
				t.Errorf("the existing message must not change: %s", x.Message)
			}
		}
	}
	if !found {
		t.Fatalf("no unmatched-trace warning: %+v", res.Warnings)
	}
}
