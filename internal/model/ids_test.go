package model_test

import (
	"slices"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Every exit's ID must round-trip through ExitFor and LookupEdge: that is
// how M3's recorder and trace matcher will get IDs, with no second way in.
func TestIDsRoundTrip(t *testing.T) {
	workflows := workflowsIn(t, "../../testdata/pilot/...")
	for _, name := range m2Workflows {
		g, err := model.Build(workflows[name])
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range g.Junctions {
			wantLabels := []string{"true", "false"}
			if j.Kind == model.ErrCheck {
				wantLabels = []string{"failure", "success"}
			}
			var labels []string
			for _, e := range j.Exits {
				labels = append(labels, e.Label)
				id, ok := g.ExitFor(j.Stmt, e.Label)
				if !ok || id != e.ID || id.String() != j.ID+"."+e.Label {
					t.Errorf("%s: ExitFor(%s, %s) = %v, %v; want %v", name, j.ID, e.Label, id, ok, e.ID)
				}
				back, ok := g.LookupEdge(e.ID.String())
				if !ok || back != e {
					t.Errorf("%s: LookupEdge(%s) did not return the same exit", name, e.ID)
				}
			}
			if !slices.Equal(labels, wantLabels) {
				t.Errorf("%s %s: exits %v, want %v", name, j.ID, labels, wantLabels)
			}
		}
		if _, ok := g.LookupEdge("J99.true"); ok {
			t.Errorf("%s: LookupEdge accepted an unknown ID", name)
		}
	}
}

// Two separate loads of the same code must give the same IDs and paths.
func TestIDsStableAcrossLoads(t *testing.T) {
	keys := func() []string {
		res, err := load.Load("../../testdata/pilot/reports")
		if err != nil {
			t.Fatal(err)
		}
		wfs := discover.Find(res.Packages)
		g, err := model.Build(wfs[0])
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			out = append(out, p.ID()+" "+p.Key())
		}
		return out
	}
	if a, b := keys(), keys(); !slices.Equal(a, b) {
		t.Errorf("paths differ between loads:\n%v\n%v", a, b)
	}
}
