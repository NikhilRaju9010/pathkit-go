package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// A switch case for string when the value is a *string is a likely defect: it
// goes to needs-review with the hint, not to cannot-be-tested. The other case
// of the same switch (int) stays impossible-type.
func TestLikelyDefectIsNeedsReviewNotImpossible(t *testing.T) {
	cl := claimsOf(withEngine(t, scope.Classification{}, enginDir, "factsengine.LikelyDefectOtherCasesStayImpossible"))
	if len(cl) != 2 {
		t.Fatalf("want 2 non-testable paths, got %v", cl)
	}
	d := cl["J1.case string|completed"]
	if d.Bucket != NeedsReview || d.Label != "likely-defect" || !d.PossibleInProduction || d.Detector != "likely-defect" {
		t.Errorf("case string: %+v", d)
	}
	if !strings.Contains(d.Explanation, "This value is a pointer (*string). Did you mean to dereference it?") {
		t.Errorf("explanation lacks the hint: %q", d.Explanation)
	}
	if len(d.Proof) == 0 || !strings.Contains(d.Proof[len(d.Proof)-1].Text, "Did you mean to dereference it?") {
		t.Errorf("the proof ends with the hint: %+v", d.Proof)
	}
	i := cl["J1.case int|completed"]
	if i.Bucket != CannotBeTested || i.Label != "impossible-type" {
		t.Errorf("case int: %+v", i)
	}
}

// Disabling the detector leaves the path ordinary (testable): never a
// cannot-be-tested claim in its place.
func TestLikelyDefectCanBeDisabled(t *testing.T) {
	cfg := scope.Classification{DisabledDetectors: []scope.DisabledDetector{{Name: "likely-defect", Reason: "test"}}}
	cl := claimsOf(withEngine(t, cfg, enginDir, "factsengine.LikelyDefectCaseString"))
	if len(cl) != 0 {
		t.Errorf("a disabled detector must leave the path testable: %v", cl)
	}
}

// Near miss: a set that holds the string too makes the case possible.
func TestPointerAndStringNoDefect(t *testing.T) {
	for k, c := range claimsOf(withEngine(t, scope.Classification{}, enginDir, "factsengine.PossibleStringAndPointer")) {
		if c.Label == "likely-defect" && k != "J1.case string|completed" {
			t.Errorf("unexpected defect on %s", k)
		}
	}
}
