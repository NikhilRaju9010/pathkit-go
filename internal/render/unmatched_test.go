package render

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func graphOf(t *testing.T, dir, name string) *model.Graph {
	t.Helper()
	res, err := load.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name == name {
			g, err := model.Build(wf)
			if err != nil {
				t.Fatal(err)
			}
			return g
		}
	}
	t.Fatalf("%s not found", name)
	return nil
}

// section runs the matcher on steps and prints the report for that one trace.
func section(t *testing.T, g *model.Graph, steps []string) string {
	t.Helper()
	_, mm := g.Match(steps)
	if mm == nil {
		t.Fatalf("%v fits a path", steps)
	}
	u := coverage.Unexplained{StepsID: coverage.StepsID(steps), Steps: steps, Count: 1, Mismatch: mm.Reason, Detail: mm}
	res := coverage.Result{Workflows: []coverage.WorkflowResult{{Name: g.Workflow, Unmatched: []coverage.Unexplained{u}}}, Counts: coverage.Counts{Unmatched: 1}}
	return UnmatchedSection(res)
}

// A step after a panic road crashed the report; a retry at
// a loop's head named the same decision twice; folded trips were silent; an
// empty trace printed nothing.
func TestUnmatchedCauses(t *testing.T) {
	panics := graphOf(t, "../../testdata/fixtures/rules", "rules.Panics")
	if out := section(t, panics, []string{"J1.true", "J1.false"}); !strings.Contains(out, "likely: the map says this road ends in a panic") ||
		!strings.Contains(out, "the map expected:  nothing: the map says this road ends in a panic") {
		t.Errorf("panic road:\n%s", out)
	}
	nested := graphOf(t, "../../testdata/fixtures/loops", "loops.NestedWorkflow")
	if out := section(t, nested, []string{"J1.retry"}); !strings.Contains(out, "likely: the run went round `for i < g.Outer` without going into it first") {
		t.Errorf("retry at the head:\n%s", out)
	}
	out := section(t, nested, []string{"J1.iterate", "J2.iterate", "J3.success", "J2.retry", "J2.exit", "J1.retry", "J1.iterate", "J9.true"})
	if !strings.Contains(out, "followed the map:  Start -> for i < g.Outer --iterate--> (repeated loop trips folded: only the last one is kept)\n") {
		t.Errorf("folded:\n%s", out)
	}
	polling := graphOf(t, "../../testdata/pilot/polling", "polling.ReportPollingWorkflow")
	if out := section(t, polling, []string{"J1.iterate", "J1.retry"}); !strings.Contains(out, "likely: the run went round `for attempt <= in.MaxPolls` again without the decision `CheckStatus (activity)` inside the loop body") {
		t.Errorf("retry inside the body:\n%s", out)
	}
	if out := section(t, polling, []string{}); !strings.Contains(out, "recorded steps:    (none: the run made no decision)\n") {
		t.Errorf("empty trace:\n%s", out)
	}
}

func TestUnmatchedBeyondCapNote(t *testing.T) {
	res := coverage.Result{Counts: coverage.Counts{Unmatched: 2}}
	if out := UnmatchedSection(res); out != "Traces that fit no path: none\n  (2 more unmatched traces on the Traces line fit paths beyond the 2000-path cap; they are not listed here)\n" {
		t.Errorf("%q", out)
	}
}
