package coverage

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

func pilotWorkflow(t *testing.T, name string) Workflow {
	t.Helper()
	res, err := load.Load("../../testdata/pilot/...")
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

// tf makes one trace file for w.
func tf(file string, w Workflow, status string, steps ...string) TraceFile {
	return TraceFile{Path: "/traces/" + file, File: trace.File{SchemaVersion: 1, Workflow: w.Name, FunctionHash: w.Hash, Status: status, Steps: steps}}
}

func covered(w WorkflowResult) []int {
	var out []int
	for _, p := range w.Paths {
		if p.Covered {
			out = append(out, p.Number)
		}
	}
	return out
}

func TestOrdersFullyCovered(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{orders}, Traces: []TraceFile{
		tf("a", orders, "complete", "J1.true"),
		tf("b", orders, "complete", "J1.false", "J2.failure"),
		tf("c", orders, "complete", "J1.false", "J2.success"),
		tf("d", orders, "complete", "J1.false", "J2.success"), // the same path again: counted once
	}})
	w := res.Workflows[0]
	if res.Paths != 3 || res.Covered != 3 || w.Branches != 4 || w.BranchesTaken != 4 {
		t.Errorf("got %d/%d paths, %d/%d branches; want 3/3 and 4/4", res.Covered, res.Paths, w.BranchesTaken, w.Branches)
	}
	if w.Paths[2].Traces != 2 || res.Counts.Counted != 4 || len(res.Warnings) != 0 {
		t.Errorf("repeated path: traces=%d counted=%d warnings=%v", w.Paths[2].Traces, res.Counts.Counted, res.Warnings)
	}
}

// Branch coverage uses the raw steps, before loop folding: "pending, then
// complete" covers only the "complete" path, but its first trip really
// took the switch's default exit and the loop's retry edge.
func TestBranchesUseRawSteps(t *testing.T) {
	polling := pilotWorkflow(t, "polling.ReportPollingWorkflow")
	res := Compute(Input{Workflows: []Workflow{polling}, Traces: []TraceFile{
		tf("pending-then-complete", polling, "complete",
			"J1.iterate", "J2.success", "J3.default", "J1.retry", "J1.iterate", "J2.success", `J3.case "complete"`),
	}})
	w := res.Workflows[0]
	if w.Covered != 1 || w.Branches != 8 || w.BranchesTaken != 5 {
		t.Fatalf("got %d paths, %d/%d branches; want 1 path and 5/8 branches", w.Covered, w.BranchesTaken, w.Branches)
	}
	// The covered path itself uses only iterate, success and case "complete".
	for _, p := range w.Paths {
		if p.Covered && len(p.Path.Steps) != 3 {
			t.Errorf("covered path %q, want the 3-step complete path", p.Path.Key())
		}
	}
}

// Every trace that doesn't count is classified and warned about; only
// the matched one counts.
func TestTraceKinds(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	stale := tf("5-stale", orders, "complete", "J1.true")
	stale.File.FunctionHash = "0000000000000000"
	res := Compute(Input{
		Workflows: []Workflow{orders},
		Excluded:  []scope.Excluded{{Name: "shipment.ShipmentWorkflow", Reason: "sandbox"}},
		Traces: []TraceFile{
			tf("1-ok", orders, "complete", "J1.true"),
			tf("2-unmatched", orders, "complete", "J2.failure"),
			tf("3-incomplete", orders, "incomplete", "J1.false"),
			{Path: "/traces/4-excluded", File: trace.File{SchemaVersion: 1, Workflow: "shipment.ShipmentWorkflow", Status: "complete"}},
			stale,
			{Path: "/traces/6-unknown", File: trace.File{SchemaVersion: 1, Workflow: "nope.Nope", Status: "complete"}},
			{Path: "/traces/7-broken", ReadErr: errors.New("not a valid trace file: unexpected end of JSON input")},
		},
	})
	want := Counts{Read: 7, Counted: 1, Unmatched: 1, Stale: 1, Incomplete: 1, Excluded: 1, Unknown: 1, Unreadable: 1}
	if res.Counts != want {
		t.Errorf("counts %+v, want %+v", res.Counts, want)
	}
	var msgs []string
	for _, w := range res.Warnings {
		msgs = append(msgs, w.Message)
	}
	wantMsgs := []string{
		`trace 2-unmatched (orders.OrderWorkflow) fits no path: step 1 "J2.failure" does not fit: the path is at J1 (if in.AmountCents <= 0)`,
		"trace 3-incomplete (orders.OrderWorkflow): the run never finished (panic, timeout, or stopped)",
		"trace 5-stale was recorded for an older version of orders.OrderWorkflow; re-run pathkit test, or pass --allow-stale",
		"trace 6-unknown: workflow nope.Nope is not among the analyzed workflows",
		"could not read trace 7-broken: not a valid trace file: unexpected end of JSON input",
	}
	if !slices.Equal(msgs, wantMsgs) {
		t.Errorf("warnings:\n  %s\nwant:\n  %s", strings.Join(msgs, "\n  "), strings.Join(wantMsgs, "\n  "))
	}
	if got := covered(res.Workflows[0]); !slices.Equal(got, []int{1}) {
		t.Errorf("covered paths %v, want only path 1 (the stale trace doesn't count)", got)
	}
}

// --allow-stale: a stale trace that still fits counts, and its path is
// marked; one that no longer fits is unmatched.
func TestAllowStale(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	fits := tf("fits", orders, "complete", "J1.true")
	fits.File.FunctionHash = "0000000000000000"
	gone := tf("gone", orders, "complete", "J9.true")
	gone.File.FunctionHash = "0000000000000000"
	res := Compute(Input{Workflows: []Workflow{orders}, AllowStale: true, Traces: []TraceFile{fits, gone}})
	p := res.Workflows[0].Paths[0]
	if !p.Covered || !p.StaleTrace || res.Counts.Counted != 1 || res.Counts.Unmatched != 1 || res.Counts.Stale != 0 {
		t.Errorf("path 1 covered=%v stale=%v, counts %+v", p.Covered, p.StaleTrace, res.Counts)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0].Message, "(a stale trace, allowed by --allow-stale) fits no path") {
		t.Errorf("warnings %+v", res.Warnings)
	}
}

// Traces of workflows --function didn't pick are counted, not "unknown".
func TestOthers(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	res := Compute(Input{Workflows: []Workflow{orders}, Others: []string{"reports.DailyReportWorkflow"}, Traces: []TraceFile{
		{Path: "/traces/r", File: trace.File{SchemaVersion: 1, Workflow: "reports.DailyReportWorkflow", Status: "complete"}},
	}})
	if res.Counts.Other != 1 || res.Counts.Unknown != 0 || len(res.Warnings) != 0 {
		t.Errorf("counts %+v warnings %v", res.Counts, res.Warnings)
	}
}

func TestPercent(t *testing.T) {
	if Percent(20, 38) < 52.63 || Percent(20, 38) > 52.64 || Percent(0, 0) != 0 {
		t.Errorf("Percent(20, 38) = %v, Percent(0, 0) = %v", Percent(20, 38), Percent(0, 0))
	}
}

// D11: the boundaries are exact, compared with whole numbers.
func TestPriority(t *testing.T) {
	tests := []struct {
		covered, total int
		want           string
	}{
		{0, 3, PriorityHigh},          // 0%
		{4999, 10000, PriorityHigh},   // 49.99%, just below 50%
		{1, 2, PriorityMedium},        // exactly 50%
		{5001, 10000, PriorityMedium}, // 50.01%
		{2, 3, PriorityMedium},        // 66.7%
		{7999, 10000, PriorityMedium}, // 79.99%, just below 80%
		{4, 5, PriorityMedium},        // exactly 80%
		{8, 10, PriorityMedium},       // exactly 80% again, other numbers
		{8001, 10000, PriorityLow},    // 80.01%, just above 80%
		{5, 6, PriorityLow},           // 83.3%
		{3, 3, PriorityLow},           // 100%
		// 1/3 is 33.33...%: never exactly representable, still High.
		{1, 3, PriorityHigh},
	}
	for _, tt := range tests {
		got, ok := Priority(tt.covered, tt.total)
		if !ok || got != tt.want {
			t.Errorf("Priority(%d, %d) = %q, %v; want %q", tt.covered, tt.total, got, ok, tt.want)
		}
	}
	if got, ok := Priority(0, 0); ok || got != "" {
		t.Errorf("Priority(0, 0) = %q, %v; want no label", got, ok)
	}
}
