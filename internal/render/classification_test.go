package render

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

func TestClassificationLines(t *testing.T) {
	res := coverage.Result{Breakdown: coverage.Breakdown{
		Testable:       coverage.BucketCount{Paths: 20, Covered: 20},
		CannotBeTested: coverage.BucketCount{Paths: 18},
		Policy:         classify.CountAsTestable,
	}}
	threshold := 100.0
	got := strings.Join(ClassificationLines(res, ClassificationView{Basis: coverage.BasisTestable, Threshold: &threshold}), "\n")
	want := `Classification:
  Total paths:         38
  Testable:            20
  Cannot be tested:    18
  Needs special test:  0
  Needs review:        0
Testable coverage:     20 / 20 = 100.0%  (testable paths only)
Special-test coverage: 0 / 0
Overall coverage:      20 / 38 = 52.6%  (all paths)
Coverage basis used:   testable (for --fail-under)`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// The basis is always named, with or without --fail-under.
	last := func(v ClassificationView) string {
		l := ClassificationLines(res, v)
		return l[len(l)-1]
	}
	if got := last(ClassificationView{Basis: coverage.BasisOverall}); got != "Coverage basis used:   overall" {
		t.Errorf("no threshold: %q", got)
	}
	if got := last(ClassificationView{Basis: coverage.BasisOverall, Threshold: &threshold}); got != "Coverage basis used:   overall (for --fail-under)" {
		t.Errorf("with threshold: %q", got)
	}
}

// Helper regions are offered as candidates next to the biggest section, only
// for a workflow whose main number is not path coverage, and only when the
// helper holds decisions that are repeated.
func TestSplitCandidatesOfferHelpers(t *testing.T) {
	w := coverage.WorkflowResult{Name: "p.W", Main: coverage.MainNumber{Kind: coverage.MainBranch},
		HelperRegions: []coverage.HelperRegion{{Name: "charge", File: "p/h.go", Line: 7, Decisions: 3, Sites: 4}, {Name: "tiny", File: "p/h.go", Line: 40, Decisions: 1, Sites: 1}}}
	got := SplitCandidates(w, nil)
	if len(got) != 1 || !strings.Contains(got[0], "charge (p/h.go:7) holds 3 decisions and is called from 4 places") {
		t.Errorf("candidates: %v", got)
	}
	w.Main.Kind = coverage.MainPath
	if got := SplitCandidates(w, nil); len(got) != 0 {
		t.Errorf("a workflow measured by path coverage needs no split advice: %v", got)
	}
}
