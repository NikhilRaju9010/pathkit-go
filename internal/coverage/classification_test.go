package coverage

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// ordersRun is the orders workflow with two of its three paths run.
func ordersRun(t *testing.T) (Workflow, []TraceFile) {
	t.Helper()
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	return w, []TraceFile{tf("a", w, "complete", "J1.true"), tf("b", w, "complete", "J1.false", "J2.failure")}
}

func TestBreakdownFormulas(t *testing.T) {
	b := Breakdown{
		Testable:         BucketCount{Paths: 20, Covered: 20},
		CannotBeTested:   BucketCount{Paths: 18},
		NeedsSpecialTest: BucketCount{Paths: 2},
		NeedsReview:      BucketCount{Paths: 3},
		Policy:           classify.CountAsTestable,
	}
	if got := b.Overall(); got != (BucketCount{Paths: 43, Covered: 20}) {
		t.Errorf("overall = %+v", got)
	}
	// Strict default: the 3 needs-review paths count in testable coverage.
	if got := b.TestableCoverage(); got != (BucketCount{Paths: 23, Covered: 20}) {
		t.Errorf("testable (count_as_testable) = %+v", got)
	}
	b.Policy = classify.Exclude
	if got := b.TestableCoverage(); got != (BucketCount{Paths: 20, Covered: 20}) {
		t.Errorf("testable (exclude) = %+v", got)
	}
	if got := b.Special(); got.Paths != 2 {
		t.Errorf("special = %+v", got)
	}
	if b.For(BasisOverall) != b.Overall() || b.For(BasisTestable) != b.TestableCoverage() {
		t.Error("For picks the wrong count")
	}
	if b.For("") != b.Overall() {
		t.Error("an empty basis must mean overall")
	}
}

// With no detector, overall and testable coverage are the same number and
// every path is testable.
func TestEveryPathStartsTestable(t *testing.T) {
	w, traces := ordersRun(t)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: traces})
	if res.Breakdown.Testable.Paths != res.Paths || res.Breakdown.Testable.Covered != res.Covered {
		t.Errorf("breakdown %+v vs %d paths, %d covered", res.Breakdown, res.Paths, res.Covered)
	}
	if res.Breakdown.Overall() != res.Breakdown.TestableCoverage() {
		t.Errorf("overall %+v != testable %+v", res.Breakdown.Overall(), res.Breakdown.TestableCoverage())
	}
	for _, p := range res.Workflows[0].Paths {
		if p.Class.Bucket != classify.Testable || !strings.HasPrefix(p.StableID, "s1:") {
			t.Errorf("path %d: %+v %q", p.Number, p.Class, p.StableID)
		}
	}
}

// A classifier that calls every path cannot-be-tested must not hide a path
// a real run took: that path is counted as testable, with a warning.
func TestCoveredPathIsNeverUntestable(t *testing.T) {
	impossible := func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label, c.Source = classify.CannotBeTested, "contradictory", classify.SourceManual
		return c
	}
	w, traces := ordersRun(t)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: traces, Classifier: impossible})
	wf := res.Workflows[0]
	if wf.Covered == 0 {
		t.Fatal("the fixture traces covered nothing")
	}
	for _, p := range wf.Paths {
		if p.Covered && p.Class.Bucket != classify.Testable {
			t.Errorf("covered path %d is %s", p.Number, p.Class.Bucket)
		}
		if !p.Covered && p.Class.Bucket != classify.CannotBeTested {
			t.Errorf("uncovered path %d is %s, want the classifier's answer", p.Number, p.Class.Bucket)
		}
	}
	if got, want := res.Breakdown.Testable.Paths, wf.Covered; got != want {
		t.Errorf("testable paths = %d, want the %d covered ones", got, want)
	}
	if got := res.Breakdown.TestableCoverage(); got.Paths != got.Covered {
		t.Errorf("testable coverage %+v: every testable path here was run", got)
	}
	// Overall coverage still sees all of them.
	if res.Breakdown.Overall().Paths != res.Paths {
		t.Errorf("overall paths = %d, want %d", res.Breakdown.Overall().Paths, res.Paths)
	}
	warned := 0
	for _, w := range res.Warnings {
		if w.Kind == "classification" {
			warned++
		}
	}
	if warned != wf.Covered {
		t.Errorf("%d classification warnings, want %d", warned, wf.Covered)
	}
}

// The totals are the sum of the workflows', for every bucket, with two
// workflows and a classifier that moves every uncovered path.
func TestBreakdownSumsAcrossWorkflows(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	pay := pilotWorkflow(t, "fulfillment.PaymentWorkflow")
	traces := []TraceFile{tf("a", orders, "complete", "J1.true"), tf("b", pay, "complete", "J1.failure")}
	special := func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket, c.Label = classify.NeedsSpecialTest, "history-limit"
		return c
	}
	res := Compute(Input{Workflows: []Workflow{orders, pay}, Traces: traces, Classifier: special})
	if res.Breakdown.Overall() != (BucketCount{Paths: res.Paths, Covered: res.Covered}) {
		t.Errorf("overall %+v, want %d paths %d covered", res.Breakdown.Overall(), res.Paths, res.Covered)
	}
	for _, k := range classify.Buckets {
		var sum BucketCount
		for _, w := range res.Workflows {
			sum.Paths += w.Breakdown.Of(k).Paths
			sum.Covered += w.Breakdown.Of(k).Covered
		}
		if sum != res.Breakdown.Of(k) {
			t.Errorf("%s: workflows sum to %+v, total says %+v", k, sum, res.Breakdown.Of(k))
		}
	}
	if res.Covered != 2 || res.Breakdown.Testable.Covered != 2 || res.Breakdown.NeedsSpecialTest.Covered != 0 {
		t.Errorf("covered paths must all be testable: %+v", res.Breakdown)
	}
}

// A stale trace allowed by --allow-stale covers a path just the same, so
// the guard applies to it too.
func TestCoveredByStaleTraceIsNeverUntestable(t *testing.T) {
	w := pilotWorkflow(t, "orders.OrderWorkflow")
	old := tf("a", w, "complete", "J1.true")
	old.File.FunctionHash = "an-older-version"
	impossible := func(string, model.Path) classify.Classification {
		c := classify.DefaultTestable()
		c.Bucket = classify.CannotBeTested
		return c
	}
	res := Compute(Input{Workflows: []Workflow{w}, Traces: []TraceFile{old}, AllowStale: true, Classifier: impossible})
	if res.Covered != 1 || res.Workflows[0].Paths[0].Class.Bucket != classify.Testable || !res.Workflows[0].Paths[0].StaleTrace {
		t.Errorf("covered=%d first path %+v", res.Covered, res.Workflows[0].Paths[0])
	}
}

// A classifier that returns no bucket is treated as testable everywhere,
// including in the path's own field (not just in the counts).
func TestEmptyBucketBecomesTestable(t *testing.T) {
	w, traces := ordersRun(t)
	res := Compute(Input{Workflows: []Workflow{w}, Traces: traces, Classifier: func(string, model.Path) classify.Classification { return classify.Classification{} }})
	for _, p := range res.Workflows[0].Paths {
		if p.Class.Bucket != classify.Testable {
			t.Errorf("path %d bucket %q", p.Number, p.Class.Bucket)
		}
	}
	if res.Breakdown.Testable.Paths != res.Paths {
		t.Errorf("%+v", res.Breakdown)
	}
}
