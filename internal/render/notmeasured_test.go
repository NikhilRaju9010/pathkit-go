package render

import (
	"math/big"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// A project whose every workflow is over the cap has no workflow under it:
// the path number must say "not measured", not "0/0 (0.0%)".
func TestHeadlineNotMeasuredWhenNoWorkflowUnderCap(t *testing.T) {
	res := coverage.Result{Workflows: []coverage.WorkflowResult{{
		Name: "w.Big", Capped: true, Exact: big.NewInt(5000), AllBranches: 4, AllBranchesTaken: 1,
	}}}
	lines := strings.Join(HeadlineLines(res, nil, true), "\n")
	if strings.Contains(lines, "0/0") {
		t.Fatalf("a 0/0 ratio is printed:\n%s", lines)
	}
	for _, want := range []string{
		"Path coverage (testable): not measured (0 workflows under the cap)",
		"Stage coverage (testable): not measured (no stage paths to measure)",
		"Branch coverage: 1/4 (25.0%)",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("missing %q in:\n%s", want, lines)
		}
	}
}

// Near miss: a ratio with a real denominator keeps its old wording.
func TestRatioKeepsNumbers(t *testing.T) {
	if got := Ratio(1, 4, nil, "x"); got != "1/4 (25.0%)" {
		t.Fatalf("got %q", got)
	}
	if got := Ratio(0, 0, nil, "no paths"); got != "not measured (no paths)" {
		t.Fatalf("got %q", got)
	}
}

func TestClassificationBlockForCappedWorkflow(t *testing.T) {
	b := coverage.Breakdown{Policy: classify.CountAsTestable}
	b.Testable = coverage.BucketCount{Paths: 723, Covered: 10}
	b.NeedsReview = coverage.BucketCount{Paths: 38}
	res := coverage.Result{Breakdown: b, Workflows: []coverage.WorkflowResult{{
		Name: "w.Big", Capped: true, Exact: big.NewInt(9000),
		Pruned: []coverage.PrunedResult{
			{Count: big.NewInt(5000), Class: classify.Classification{Label: "contradictory"}},
			{Count: big.NewInt(7), Class: classify.Classification{Label: "contradictory"}, Unpruned: true},
		},
	}}}
	out := strings.Join(ClassificationLines(res, ClassificationView{Basis: coverage.BasisTestable}), "\n")
	for _, want := range []string{
		"Classification (listed paths only; 1 workflow over the cap):",
		"Cannot be tested:    0 listed, plus 5,000 left out of the list as impossible",
		"Left out of the lists as impossible: 5,000 paths in 1 group",
		"contradictory: 5,000",
		"Testable 723 + needs-review 38 = 761 in the denominator (policy: count_as_testable)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Near miss: with no workflow over the cap the block is the old one.
	res.Workflows[0].Capped = false
	res.Workflows[0].Pruned = nil
	old := strings.Join(ClassificationLines(res, ClassificationView{Basis: coverage.BasisTestable}), "\n")
	if strings.Contains(old, "listed paths only") || strings.Contains(old, "denominator") || strings.Contains(old, "Left out") {
		t.Errorf("under-cap block changed:\n%s", old)
	}
}
