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
