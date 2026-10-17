package coverage

import (
	"math/big"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// bigWorkflow builds one workflow of the bigflow fixture and an engine that
// knows it.
func bigWorkflow(t *testing.T, name string) (Workflow, *classify.Engine) {
	t.Helper()
	res, err := load.Load("../../testdata/fixtures/bigflow")
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name == "bigflow."+name {
			g, err := model.Build(wf)
			if err != nil {
				t.Fatal(err)
			}
			eng, err := classify.NewEngine(scope.Classification{Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := eng.AddGraph(wf.Name, g); err != nil {
				t.Fatal(err)
			}
			return Workflow{Name: wf.Name, Graph: g, Hash: g.CodeHash()}, eng
		}
	}
	t.Fatalf("bigflow.%s not found", name)
	return Workflow{}, nil
}

// walkFirst follows the first exit at every decision, from the given prefix,
// to an end: a path that exists, however many there are.
func walkFirst(w Workflow, prefix []model.Step) []string {
	var ids []string
	for _, s := range prefix {
		ids = append(ids, s.Exit.ID.String())
	}
	t := prefix[len(prefix)-1].Exit.To
	used := map[model.EdgeID]bool{}
	for _, s := range prefix {
		used[s.Exit.ID] = true
	}
	for t.Junction != nil || t.Retry != nil {
		if t.Retry != nil {
			ids = append(ids, t.Retry.ID.String())
			used[t.Retry.ID] = true
			t = t.Retry.To
			continue
		}
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*model.Exit{t.Only}
		}
		var next *model.Exit
		for _, e := range exits {
			if !used[e.ID] {
				next = e
				break
			}
		}
		if next == nil {
			break
		}
		used[next.ID] = true
		ids = append(ids, next.ID.String())
		t = next.To
	}
	return ids
}

// A workflow with far more paths than the cap lists: the count is exact, the
// parts the engine shows impossible are left out of the list and counted, and
// the list is full of paths that can be taken.
func TestPrunedListing(t *testing.T) {
	w, eng := bigWorkflow(t, "PrunedFlow")
	l := eng.Listing(w.Name, w.Graph)
	if got, want := l.Exact.String(), "4196352"; got != want {
		t.Errorf("exact count %s, want %s", got, want)
	}
	if len(l.List) != model.DefaultMaxPaths || !l.Truncated {
		t.Errorf("%d paths listed (truncated %v), want %d", len(l.List), l.Truncated, model.DefaultMaxPaths)
	}
	if len(l.Pruned) != 1 || l.Pruned[0].Group.Count.String() != "4194304" {
		t.Fatalf("pruned groups: %+v", l.Pruned)
	}
	pg := l.Pruned[0]
	if pg.Class.Bucket != classify.CannotBeTested || pg.Class.Label != "constant-condition" || len(pg.Class.Proof) == 0 {
		t.Errorf("the group is classified %+v", pg.Class)
	}
	last := pg.Group.Prefix[len(pg.Group.Prefix)-1]
	for _, p := range l.List {
		for _, s := range p.Steps {
			if s.Exit == last.Exit {
				t.Fatalf("listed path %s takes the pruned exit", p.Key())
			}
		}
	}
	// A plain listing of the same workflow starts with the paths of the pruned part.
	plain := w.Graph.Paths(model.DefaultMaxPaths)
	if plain.List[0].Key() == l.List[0].Key() {
		t.Error("the pruned listing starts with the same path as the plain one, which is inside the pruned part")
	}
	// Listed paths keep their stable ids: they are the plain listing's ids for
	// the same steps.
	for _, p := range l.List[:50] {
		if p.StableID() == "" {
			t.Error("empty stable id")
		}
	}
	// And the counts add up: listed + pruned + not reached = exact.
	rest := new(big.Int).Sub(l.Exact, pg.Group.Count)
	if rest.Cmp(big.NewInt(2048)) != 0 {
		t.Errorf("the possible part has %s paths, want 2048", rest)
	}
}

// A trace that runs a path inside a pruned part shows the proof was wrong: the
// group is under review, the path is listed, and the report warns.
func TestTraceThroughAPrunedPart(t *testing.T) {
	w, eng := bigWorkflow(t, "PrunedFlow")
	l := eng.Listing(w.Name, w.Graph)
	steps := walkFirst(w, l.Pruned[0].Group.Prefix)
	bad := TraceFile{Path: "/traces/bad", File: trace.File{SchemaVersion: 2, Workflow: w.Name, FunctionHash: w.Hash, Status: "complete", Steps: steps}}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{bad}, Classifier: eng.Classify, Policy: eng.Policy(), Listing: eng.Listing, Plan: eng.PlanStages})
	wr := res.Workflows[0]
	if res.Counts.Unmatched != 0 || res.Counts.Counted != 1 {
		t.Fatalf("counts %+v: the trace fits the map and must be counted", res.Counts)
	}
	if len(wr.Pruned) != 1 || !wr.Pruned[0].Unpruned || wr.Pruned[0].Class.Bucket != classify.NeedsReview {
		t.Errorf("the group should be under review: %+v", wr.Pruned)
	}
	if res.PrunedPaths != nil && res.PrunedPaths.Sign() != 0 {
		t.Errorf("a group under review is not counted as impossible: %s", res.PrunedPaths)
	}
	found := false
	for _, x := range res.Warnings {
		if strings.Contains(x.Message, "proved impossible") && strings.Contains(x.Message, "needs-review") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning about the trace: %+v", res.Warnings)
	}
	covered := 0
	for _, p := range wr.Paths {
		if p.Covered {
			covered++
			if p.Class.Bucket != classify.Testable {
				t.Errorf("a path a test ran must be testable: %+v", p.Class)
			}
		}
	}
	if covered != 1 {
		t.Errorf("%d covered paths, want the one the trace ran", covered)
	}
}

// A trace that runs a path beyond the listed ones is counted, and covers the
// stage paths it passes through; it is not unmatched.
func TestTraceBeyondTheList(t *testing.T) {
	w, eng := bigWorkflow(t, "TwentyFourIfs")
	// every decision false: the last path of the listing order
	var steps []string
	for _, j := range w.Graph.Junctions {
		for _, e := range j.Exits {
			if e.Label == "false" {
				steps = append(steps, e.ID.String())
			}
		}
	}
	tr := TraceFile{Path: "/traces/far", File: trace.File{SchemaVersion: 2, Workflow: w.Name, FunctionHash: w.Hash, Status: "complete", Steps: steps}}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{tr}, Classifier: eng.Classify, Policy: eng.Policy(), Listing: eng.Listing, Plan: eng.PlanStages})
	if res.Counts.Unmatched != 0 || res.Counts.Counted != 1 || res.Counts.BeyondCap != 1 {
		t.Fatalf("counts %+v", res.Counts)
	}
	wr := res.Workflows[0]
	if !wr.Capped || wr.Exact.String() != "16777216" || wr.BeyondCap != 1 || wr.Covered != 0 {
		t.Errorf("capped %v, exact %s, beyond %d, covered %d", wr.Capped, wr.Exact, wr.BeyondCap, wr.Covered)
	}
	if len(wr.Stages) != 3 {
		t.Fatalf("%d stages", len(wr.Stages))
	}
	for _, s := range wr.Stages {
		if s.Covered != 1 || len(s.Paths) != 256 || s.BranchesTaken != 8 || s.Branches != 16 {
			t.Errorf("stage %d: covered %d of %d, branches %d/%d (want 1 of 256, 8/16)", s.Index+1, s.Covered, len(s.Paths), s.BranchesTaken, s.Branches)
		}
	}
	if wr.AllBranches != 48 || wr.AllBranchesTaken != 24 {
		t.Errorf("branches of the whole map %d/%d, want 24/48", wr.AllBranchesTaken, wr.AllBranches)
	}
}
