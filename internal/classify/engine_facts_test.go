package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const (
	shapesDir = "../../testdata/shapes"
	enginDir  = "../../testdata/fixtures/factsengine"
)

// withEngine classifies every path of one workflow with the fact engine
// switched on (the graph is given to the engine, as the CLI does).
func withEngine(t *testing.T, cfg scope.Classification, folder, name string) []detectedPath {
	t.Helper()
	cfg.Enabled = true
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g := graph(t, folder, name)
	if err := e.AddGraph(name, g); err != nil {
		t.Fatal(err)
	}
	var out []detectedPath
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		out = append(out, detectedPath{p, e.Classify(name, p)})
	}
	return out
}

func claimsOf(ds []detectedPath) map[string]Classification {
	out := map[string]Classification{}
	for _, d := range ds {
		if d.C.Bucket != Testable {
			out[d.Path.Key()] = d.C
		}
	}
	return out
}

// Shape (a): the engine claims the path, with its label, confidence, who
// decided, and a proof.
func TestEngineClaimsValueFlow(t *testing.T) {
	cl := claimsOf(withEngine(t, scope.Classification{}, shapesDir+"/valueflow", "valueflow.ContradictionThroughAssign"))
	if len(cl) != 1 {
		t.Fatalf("want exactly 1 claim, got %v", cl)
	}
	c := cl["J1.false J2.false|completed"]
	if c.Bucket != CannotBeTested || c.Label != "value-flow" || c.Confidence != Certain || c.Source != SourceAuto || c.Detector != "value-flow" || c.PossibleInProduction {
		t.Errorf("claim: %+v", c)
	}
	if len(c.Proof) < 3 || !strings.Contains(c.Proof[1].Text, "`id = v` is skipped") {
		t.Errorf("proof: %+v", c.Proof)
	}
	if !strings.Contains(c.Explanation, "always true") {
		t.Errorf("explanation: %q", c.Explanation)
	}
	// Near-miss: an unknown call changes id after the copy; only the claim
	// that still follows (ok false leaves id alone is undone by the call) stays testable.
	for key, c := range claimsOf(withEngine(t, scope.Classification{}, shapesDir+"/valueflow", "valueflow.ReassignedByCall")) {
		t.Errorf("near-miss claimed: %s %+v", key, c)
	}
}

// The three labels of an engine claim.
func TestEngineLabels(t *testing.T) {
	cases := []struct {
		folder, workflow, key, label string
	}{
		{shapesDir + "/zerovalue", "zerovalue.NeverAssigned", "J1.false|completed", "constant-condition"},
		{shapesDir + "/valueflow", "valueflow.ContradictionThroughAssign", "J1.false J2.false|completed", "value-flow"},
		{shapesDir + "/guardedtwice", "guardedtwice.LengthCheckAfterGuard", "J1.false J2.true|completed", "value-flow"},
		{enginDir, "factsengine.ImpossibleBranchContradiction", "J1.true J2.true|completed", "contradictory"},
	}
	for _, c := range cases {
		cl := claimsOf(withEngine(t, scope.Classification{}, c.folder, c.workflow))
		if got := cl[c.key]; got.Label != c.label || got.Bucket != CannotBeTested {
			t.Errorf("%s %s: %+v, want cannot-be-tested/%s", c.workflow, c.key, got, c.label)
		}
	}
}

// A detector that already claims the path keeps its label and its wording;
// the engine adds nothing, not even an extra reason.
func TestDetectorWinsOverEngine(t *testing.T) {
	const folder = "../../testdata/fixtures/dataflow"
	for _, name := range []string{"dataflow.TwiceLocal", "dataflow.SameConditionTwice"} {
		with := withEngine(t, scope.Classification{}, folder, name)
		without := detected(t, folder, name)
		for i := range with {
			a, b := with[i].C, without[i].C
			if a.Label != b.Label || a.Explanation != b.Explanation || a.Detector != b.Detector || len(a.ExtraReasons) != len(b.ExtraReasons) || len(a.Proof) != len(b.Proof) {
				t.Errorf("%s %s changed:\n with    %+v\n without %+v", name, with[i].Path.Key(), a, b)
			}
		}
	}
}

// The engine can be turned off: for everything in the config, or by name.
func TestEngineSwitches(t *testing.T) {
	const f = shapesDir + "/valueflow"
	const w = "valueflow.ContradictionThroughAssign"
	for name, cfg := range map[string]scope.Classification{
		"engine off":            {EngineOff: true},
		"value-flow disabled":   {DisabledDetectors: []scope.DisabledDetector{{Name: "value-flow", Reason: "test"}}},
		"disabled by name only": {DisabledDetectors: []scope.DisabledDetector{{Name: "value-flow", Reason: "x"}, {Name: "possible-correlation", Reason: "y"}}},
	} {
		if cl := claimsOf(withEngine(t, cfg, f, w)); len(cl) != 0 {
			t.Errorf("%s: claims %v", name, cl)
		}
	}
}

// A suspect: needs-review, likely, naming both steps; turned off by name.
func TestPossibleCorrelation(t *testing.T) {
	const w = "factsengine.SuspectLoopBetween"
	cl := claimsOf(withEngine(t, scope.Classification{}, enginDir, w))
	c, ok := cl["J1.true J2.true|completed"]
	if len(cl) != 1 || !ok {
		t.Fatalf("want one suspect, got %v", cl)
	}
	if c.Bucket != NeedsReview || c.Label != "possible-correlation" || c.Confidence != Likely || !c.PossibleInProduction || c.Detector != "possible-correlation" {
		t.Errorf("suspect: %+v", c)
	}
	if !strings.Contains(c.Explanation, "Step 1 and step 2") || !strings.Contains(c.Explanation, "may change it") {
		t.Errorf("explanation does not name both steps and the reason: %q", c.Explanation)
	}
	off := scope.Classification{DisabledDetectors: []scope.DisabledDetector{{Name: "possible-correlation", Reason: "test"}}}
	if cl := claimsOf(withEngine(t, off, enginDir, w)); len(cl) != 0 {
		t.Errorf("disabled: %v", cl)
	}
}

// The new detector names are accepted in the config, and a typo still lists them.
func TestNewDetectorNames(t *testing.T) {
	for _, n := range []string{"value-flow", "possible-correlation"} {
		if err := CheckDetectorNames([]scope.DisabledDetector{{Name: n, Reason: "r"}}); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	err := CheckDetectorNames([]scope.DisabledDetector{{Name: "value-flo", Reason: "r"}})
	if err == nil || !strings.Contains(err.Error(), "value-flow") || !strings.Contains(err.Error(), "possible-correlation") {
		t.Errorf("err = %v", err)
	}
}

// The safety fixtures stay testable with the engine on, on every path.
func TestSafetyShapesStayTestable(t *testing.T) {
	for _, c := range []struct{ dir, wf string }{
		{"addrtaken", "addrtaken.GetFillsVariable"},
		{"closurewrite", "closurewrite.WrittenInGo"},
		{"closurewrite", "closurewrite.WrittenInSelectorCallback"},
		{"closurewrite", "closurewrite.WrittenInDefer"},
		{"ptrreceiver", "ptrreceiver.PointerReceiverCall"},
		{"loopchange", "loopchange.ChangedInBody"},
		{"loopchange", "loopchange.CountedLoop"},
		{"sidesignal", "sidesignal.SideEffectValue"},
		{"sidesignal", "sidesignal.SignalValue"},
		{"recoverfn", "recoverfn.UsesRecover"},
		{"notunderstood", "notunderstood.CallResult"},
		{"pointerfield", "pointerfield.FieldThroughPointer"},
		{"unsafefn", "unsafefn.UsesUnsafe"},
		{"skipregions", "skipregions.ErrorCheckBody"},
		{"skipregions", "skipregions.IgnoredIf"},
		{"skipregions", "skipregions.LoopWalkedOnce"},
	} {
		if cl := claimsOf(withEngine(t, scope.Classification{}, shapesDir+"/"+c.dir, c.wf)); len(cl) != 0 {
			t.Errorf("%s must stay testable, got %v", c.wf, cl)
		}
	}
}
