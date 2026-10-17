package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const defectDir = "../../testdata/shapes/knowndefect"
const defectFlow = "knowndefect.BugBlockedBranch"

// A rule with the label known-defect puts the paths it names in
// needs-review (never cannot-be-tested), and beats PathKit's own claim.
func TestKnownDefectRuleIsNeedsReview(t *testing.T) {
	g := graph(t, defectDir, defectFlow)
	e, err := NewEngine(scope.Classification{Enabled: true, Rules: []scope.Rule{{
		ID: "eu-blocked", Workflow: defectFlow, Label: KnownDefect,
		When: []string{`if region == "eu" --true`}, Reason: "region is overwritten",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range classifyAll(e, defectFlow, g) {
		if c.Label != KnownDefect {
			if c.Bucket != Testable {
				t.Errorf("a path the rule does not name is %s/%s", c.Bucket, c.Label)
			}
			continue
		}
		n++
		if c.Bucket != NeedsReview || !c.PossibleInProduction {
			t.Errorf("known-defect path: bucket %s, possible %v; want needs-review and possible", c.Bucket, c.PossibleInProduction)
		}
		if !strings.Contains(c.Explanation, "region is overwritten") {
			t.Errorf("explanation %q does not carry the reason", c.Explanation)
		}
	}
	if n != 2 {
		t.Errorf("%d paths marked known-defect, want 2", n)
	}
}

// Without the rule PathKit proves the eu branch cannot run: that is
// cannot-be-tested, not known-defect. Only a person can say "bug".
func TestKnownDefectIsNeverSetByPathKit(t *testing.T) {
	g := graph(t, defectDir, defectFlow)
	e, err := NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range classifyAll(e, defectFlow, g) {
		if c.Label == KnownDefect {
			t.Errorf("PathKit set known-defect by itself: %+v", c)
		}
	}
}

// An override with the label known-defect works the same way.
func TestKnownDefectOverride(t *testing.T) {
	g := graph(t, defectDir, defectFlow)
	var id string
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if strings.Contains(p.Key(), "J2.true") {
			id = p.StableID()
			break
		}
	}
	e, err := NewEngine(scope.Classification{Enabled: true, Overrides: []scope.PathOverride{
		{Workflow: defectFlow, StableID: id, Untestable: true, Label: KnownDefect, Reason: "bug 1"}}})
	if err != nil {
		t.Fatal(err)
	}
	c := classifyAll(e, defectFlow, g)[id]
	if c.Bucket != NeedsReview || c.Label != KnownDefect {
		t.Errorf("got %s/%s, want needs-review/known-defect", c.Bucket, c.Label)
	}
}
