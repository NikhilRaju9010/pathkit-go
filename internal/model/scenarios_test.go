package model_test

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The count made from the shape of the map (the automaton) must equal the
// number of distinct fingerprints among the listed paths, for every fixture
// workflow with few enough paths to list: that is what "scenario" means.
func TestScenarioCountMatchesListedPaths(t *testing.T) {
	checked, measured := 0, 0
	for _, dir := range []string{"helpers", "loops", "saga", "switches", "waits", "versiongate", "returncall", "unread", "stable", "summarize", "helperwrites", "panics", "rules"} {
		for name, wf := range workflowsIn(t, "../../testdata/fixtures/"+dir) {
			g, err := model.Build(wf)
			if err != nil {
				continue
			}
			list := g.Paths(5000)
			if list.Truncated {
				continue
			}
			checked++
			set := g.MeasureScenarios(10000, 0)
			if set.Unmeasured != "" {
				// a workflow with unread places: its paths must all differ
				seen := map[string]bool{}
				for _, p := range list.List {
					seen[model.ScenarioID(g.Fingerprint(p))] = true
				}
				if len(seen) != len(list.List) {
					t.Errorf("%s: not measured (%s) but two paths share a fingerprint", name, set.Unmeasured)
				}
				continue
			}
			measured++
			want := map[string]bool{}
			for _, p := range list.List {
				want[model.ScenarioID(g.Fingerprint(p))] = true
			}
			got := map[string]bool{}
			for _, w := range set.Listed {
				got[model.ScenarioID(w)] = true
			}
			if set.Count.Int64() != int64(len(want)) || len(got) != len(want) {
				t.Errorf("%s: automaton says %v scenarios, the %d listed paths make %d", name, set.Count, len(list.List), len(want))
			}
			for id := range want {
				if !got[id] {
					t.Errorf("%s: a listed path's scenario %s is not in the automaton", name, id)
				}
			}
		}
	}
	if measured < 20 {
		t.Fatalf("only %d of %d workflows were measured; the check is not looking at enough", measured, checked)
	}
	t.Logf("%d workflows checked, %d measured", checked, measured)
}

func TestFingerprintReadsSteps(t *testing.T) {
	wf := workflowsIn(t, "../../testdata/fixtures/loops")
	for name, w := range wf {
		g, err := model.Build(w)
		if err != nil {
			continue
		}
		for _, p := range g.Paths(50).List {
			fp := g.Fingerprint(p)
			if len(fp) == 0 || !strings.HasPrefix(fp[len(fp)-1], "end") && !strings.HasPrefix(fp[len(fp)-1], "unknown") {
				t.Errorf("%s: fingerprint %v does not end with the end", name, fp)
			}
		}
	}
}

// No id repeats within a workflow: not a path's stable id, not a stage path's
// id, not a scenario's id, in any fixture workflow (the ones with helper copies
// included).
func TestNoDuplicateIDs(t *testing.T) {
	seenWorkflows := 0
	for _, dir := range []string{"helpers", "loops", "saga", "switches", "waits", "versiongate", "returncall", "unread", "stable", "summarize", "helperwrites", "panics", "rules", "scenarios", "bigflow"} {
		for name, wf := range workflowsIn(t, "../../testdata/fixtures/"+dir) {
			for _, mode := range []model.HelperMode{model.HelpersInline, model.HelpersSummarize} {
				g, err := model.BuildWith(wf, model.Options{Helpers: mode})
				if err != nil {
					continue
				}
				seenWorkflows++
				list := g.Paths(model.DefaultMaxPaths)
				ids := map[string]string{}
				for _, p := range list.List {
					if prev, dup := ids[p.StableID()]; dup {
						t.Errorf("%s (%v): two paths share the stable id %s:\n %s\n %s", name, mode, p.StableID(), prev, p.Key())
					}
					ids[p.StableID()] = p.Key()
				}
				for _, st := range g.Stages(model.DefaultStageMaxPaths) {
					if st.Big {
						continue
					}
					sids := map[string]bool{}
					sps, _ := g.StagePaths(st, 5000)
					for _, sp := range sps {
						if sids[sp.StageID()] {
							t.Errorf("%s (%v): two stage paths share the id %s", name, mode, sp.StageID())
						}
						sids[sp.StageID()] = true
					}
				}
				set := g.MeasureScenarios(5000, 0)
				scen := map[string]bool{}
				for _, w := range set.Listed {
					id := model.ScenarioID(w)
					if scen[id] {
						t.Errorf("%s (%v): two scenarios share the id %s", name, mode, id)
					}
					scen[id] = true
				}
			}
		}
	}
	if seenWorkflows < 100 {
		t.Fatalf("only %d workflows looked at", seenWorkflows)
	}
}
