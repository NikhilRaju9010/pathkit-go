package model_test

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func buildSummarize(t *testing.T, name string, mode model.HelperMode) *model.Graph {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/fixtures/summarize")["summarize."+name]
	if !ok {
		t.Fatalf("fixture workflow summarize.%s not found", name)
	}
	g, err := model.BuildWith(wf, model.Options{Helpers: mode})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// With the helper copied in, Chain has 8191 paths; auto mode sees that this
// is more than the cap and maps the helper once.
func TestAutoSummarizesOnlyWhenInliningPassesTheCap(t *testing.T) {
	inline := buildSummarize(t, "Chain", model.HelpersInline)
	if n := inline.PathCount().String(); n != "8191" {
		t.Fatalf("inline Chain has %s paths, want 8191", n)
	}
	auto := buildSummarize(t, "Chain", model.HelpersAuto)
	if !auto.Summarized() || auto.SummarizedBecause() == "" {
		t.Fatalf("Chain should be summarized in auto mode")
	}
	if n := auto.PathCount().String(); n != "13" {
		t.Errorf("summarized Chain has %s paths, want 13", n)
	}
	units := auto.Units()
	if len(units) != 1 || units[0].Name() != "pause" || len(units[0].Calls) != 12 || len(units[0].Branches()) != 2 {
		t.Fatalf("units: %+v", units)
	}
	if units[0].ID != "H1" || units[0].Junctions[0].ID != "H1.J1" {
		t.Errorf("ids: %s %s", units[0].ID, units[0].Junctions[0].ID)
	}
	// Near miss: Small stays under the cap, so auto keeps the helpers copied in
	// and its paths are exactly what they were.
	small := buildSummarize(t, "Small", model.HelpersAuto)
	if small.Summarized() || len(small.Units()) != 0 {
		t.Errorf("Small was summarized")
	}
	if buildSummarize(t, "Small", model.HelpersInline).PathCount().String() != small.PathCount().String() {
		t.Errorf("Small's paths changed in auto mode")
	}
}

// The workflow's own decisions keep their numbers whatever the helpers do.
func TestSummarizedHelpersDoNotNumberWorkflowJunctions(t *testing.T) {
	g := buildSummarize(t, "Chain", model.HelpersSummarize)
	for i, j := range g.Junctions {
		if j.Site != nil {
			t.Errorf("junction %s is a helper's decision in the workflow list", j.ID)
		}
		if want := "J" + string(rune('1'+i%9)); i < 9 && j.ID != want {
			t.Errorf("junction %d is %s, want %s", i, j.ID, want)
		}
	}
}

// Nested helpers are units too; a helper that calls itself is not followed.
func TestNestedHelpersAreUnits(t *testing.T) {
	g := buildSummarize(t, "Nested", model.HelpersSummarize)
	names := map[string]int{}
	for _, u := range g.Units() {
		names[u.Name()] = len(u.Calls)
	}
	if names["outer"] != 1 || names["pause"] != 1 || names["notify"] != 1 {
		t.Errorf("units: %v", names)
	}
}
