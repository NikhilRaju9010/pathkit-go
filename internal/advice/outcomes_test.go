package advice

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

func ordersResult(t *testing.T, steps ...string) coverage.Result {
	t.Helper()
	res, err := load.Load("../../testdata/pilot/...")
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name != "orders.OrderWorkflow" {
			continue
		}
		g, err := model.Build(wf)
		if err != nil {
			t.Fatal(err)
		}
		w := coverage.Workflow{Name: wf.Name, Graph: g, Hash: g.CodeHash()}
		var traces []coverage.TraceFile
		if len(steps) > 0 {
			traces = append(traces, coverage.TraceFile{Path: "/t/a", File: trace.File{SchemaVersion: 1, Workflow: wf.Name, FunctionHash: w.Hash, Status: "complete", Steps: steps}})
		}
		return coverage.Compute(coverage.Input{Workflows: []coverage.Workflow{w}, Traces: traces})
	}
	t.Fatal("orders workflow not found")
	return coverage.Result{}
}

// With one test run, every missed outcome is listed once with a place, a plain
// sentence and a test name; covered outcomes are not listed; the riskiest first.
func TestMissedOutcomesOfOrders(t *testing.T) {
	res := ordersResult(t, "J1.false", "J2.success")
	items := MissedOutcomes(res, nil, 0)
	missed := 0
	for _, w := range res.Workflows {
		for _, o := range w.Outcomes {
			if o.Status == coverage.OutcomeMissed {
				missed++
			}
		}
	}
	if missed == 0 || len(items) != missed {
		t.Fatalf("%d items for %d missed outcomes", len(items), missed)
	}
	names := map[string]bool{}
	for i, it := range items {
		if it.Rank != i+1 || it.ID == "" || it.Sentence == "" || it.File == "" || it.Line == 0 || !strings.HasPrefix(it.TestName, "Test") || it.Risk == "" {
			t.Errorf("incomplete item: %+v", it)
		}
		if names[it.TestName] {
			t.Errorf("test name %s used twice", it.TestName)
		}
		names[it.TestName] = true
		if i > 0 && tagRank(items[i-1].Risk) > tagRank(it.Risk) {
			t.Errorf("not riskiest first: %s before %s", items[i-1].Risk, it.Risk)
		}
	}
	if got := MissedOutcomes(res, nil, 1); len(got) != 1 {
		t.Errorf("limit 1 gave %d", len(got))
	}
	// near miss: nothing is missed once every outcome is taken
	all := ordersResult(t, "J1.true")
	if len(MissedOutcomes(all, nil, 0)) == 0 {
		t.Error("one short run leaves outcomes missed")
	}
}

func TestSentencesArePlainWords(t *testing.T) {
	for _, c := range []struct {
		o    coverage.OutcomeResult
		want string
	}{
		{coverage.OutcomeResult{Kind: model.PlainIf, Decision: "if amount > limit", Outcome: "true"}, "`amount > limit` was never true"},
		{coverage.OutcomeResult{Kind: model.ErrCheck, Decision: "SaveRecord (activity)", Outcome: "failure", Call: "SaveRecord", FailureKind: "timeout"}, "SaveRecord never failed with a timeout here"},
		{coverage.OutcomeResult{Kind: model.ErrCheck, Decision: "SaveRecord (activity)", Outcome: "success", Call: "SaveRecord"}, "SaveRecord never succeeded here"},
		{coverage.OutcomeResult{Kind: model.Selector, Decision: "select", Outcome: "timeout"}, "The wait never timed out"},
	} {
		if got := Sentence(c.o); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
