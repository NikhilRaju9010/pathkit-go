package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const (
	withoutFixtures = "../../testdata/fixtures/rulewithout"
	listWF          = "rulewithout.ListWorkflow"
	twiceWF         = "rulewithout.TwiceWorkflow"
)

func ruleEngine(t *testing.T, r scope.Rule) *Engine {
	t.Helper()
	e, err := NewEngine(scope.Classification{Enabled: true, Rules: []scope.Rule{r}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// marked counts the paths the engine's rule claims, and returns them.
func marked(e *Engine, name string, g *model.Graph) (n int, paths []model.Path) {
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if c := e.Classify(name, p); c.Bucket == CannotBeTested && c.Origin == OriginRule {
			n++
			paths = append(paths, p)
		}
	}
	return n, paths
}

func hasDecision(p model.Path, decision string) bool {
	for _, d := range p.Decisions() {
		if d == decision {
			return true
		}
	}
	return false
}

// "when" alone cannot single out "the loop did not run": the same exit also
// appears on a path that ran the loop and then finished it. "without" can.
func TestWithoutRemovesPathsThatTakeTheDecision(t *testing.T) {
	g := graph(t, withoutFixtures, listWF)
	const exit, iterate = "for range items --exit", "for range items --iterate"

	whenOnly := ruleEngine(t, scope.Rule{ID: "r", Workflow: listWF, Label: "same-list-loops", When: []string{exit}, Reason: "x"})
	if err := whenOnly.AddGraph(listWF, g); err != nil {
		t.Fatal(err)
	}
	nAll, all := marked(whenOnly, listWF, g)

	with := ruleEngine(t, scope.Rule{ID: "r", Workflow: listWF, Label: "same-list-loops", When: []string{exit}, Without: []string{iterate}, Reason: "x"})
	if err := with.AddGraph(listWF, g); err != nil {
		t.Fatal(err)
	}
	n, got := marked(with, listWF, g)

	skipped := 0
	for _, p := range all {
		if !hasDecision(p, iterate) {
			skipped++
		}
	}
	if skipped == 0 || skipped == nAll {
		t.Fatalf("the fixture must have paths with the exit both with and without the iterate (%d of %d)", skipped, nAll)
	}
	if n != skipped {
		t.Errorf("with \"without\": %d paths marked, want %d (the paths that take the exit and never iterate); without it %d", n, skipped, nAll)
	}
	for _, p := range got {
		if hasDecision(p, iterate) {
			t.Errorf("a path that takes %q was marked: %s", iterate, p.Key())
		}
	}
	// the rule worked without a graph given too (exact text, as before)
	plain := ruleEngine(t, scope.Rule{ID: "r", Workflow: listWF, Label: "same-list-loops", When: []string{exit}, Without: []string{iterate}, Reason: "x"})
	if m, _ := marked(plain, listWF, g); m != n {
		t.Errorf("no graph given: %d marked, want %d", m, n)
	}
}

func TestPlainTextThatFitsSeveralDecisionsIsAnError(t *testing.T) {
	g := graph(t, withoutFixtures, twiceWF)
	try := func(when ...string) error {
		e := ruleEngine(t, scope.Rule{ID: "r", Workflow: twiceWF, Label: "contradictory", When: when, Reason: "x"})
		return e.AddGraph(twiceWF, g)
	}

	err := try("if a || b --true")
	if err == nil {
		t.Fatal("a plain text that fits two decisions must be an error")
	}
	for _, want := range []string{`"if a || b --true"`, `"if a || b #2 --true"`, "#1", `any: if a || b --true`, `rule "r"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %s: %v", want, err)
		}
	}
	for _, ok := range []string{"if a || b #1 --true", "if a || b #2 --true", "any: if a || b --true"} {
		if err := try(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	// a text that fits one decision stays valid as it was
	if err := try("ExecuteActivity first --success"); err != nil && strings.Contains(err.Error(), "different decisions") {
		t.Errorf("a unique text must not be called ambiguous: %v", err)
	}
	// "without" follows the same rule
	e := ruleEngine(t, scope.Rule{ID: "r", Workflow: twiceWF, Label: "contradictory", When: []string{"if a || b #2 --true"}, Without: []string{"if a || b --true"}, Reason: "x"})
	if err := e.AddGraph(twiceWF, g); err == nil || !strings.Contains(err.Error(), `"without"`) {
		t.Errorf("an ambiguous \"without\" entry must be an error naming \"without\": %v", err)
	}
}

// #1 and #2 are different decisions, and "any:" is their union.
func TestNumberedAndAnyMatchTheRightPaths(t *testing.T) {
	g := graph(t, withoutFixtures, twiceWF)
	count := func(when string) int {
		e := ruleEngine(t, scope.Rule{ID: "r", Workflow: twiceWF, Label: "contradictory", When: []string{when}, Reason: "x"})
		if err := e.AddGraph(twiceWF, g); err != nil {
			t.Fatal(err)
		}
		n, _ := marked(e, twiceWF, g)
		return n
	}
	first, second, either := count("if a || b #1 --false"), count("if a || b #2 --false"), count("any: if a || b --false")
	if first == 0 || second == 0 || either < first || either < second || either > first+second {
		t.Errorf("first %d, second %d, any %d", first, second, either)
	}
}

func TestWithoutNamingNoDecisionIsAnError(t *testing.T) {
	g := graph(t, withoutFixtures, listWF)
	e := ruleEngine(t, scope.Rule{ID: "r", Workflow: listWF, Label: "same-list-loops",
		When: []string{"for range items --exit"}, Without: []string{"for range itmes --iterate"}, Reason: "x"})
	err := e.AddGraph(listWF, g)
	if err == nil || !strings.Contains(err.Error(), "names no decision") || !strings.Contains(err.Error(), "itmes") {
		t.Errorf("a misspelled \"without\" would widen the rule; it must be an error: %v", err)
	}
	// a misspelled "when" only makes the rule match nothing (reported as stale)
	e = ruleEngine(t, scope.Rule{ID: "r", Workflow: listWF, Label: "same-list-loops", When: []string{"for range itmes --exit"}, Reason: "x"})
	if err := e.AddGraph(listWF, g); err != nil {
		t.Errorf("a misspelled \"when\" is stale, not an error: %v", err)
	}
	if n, _ := marked(e, listWF, g); n != 0 {
		t.Errorf("%d paths marked by a rule that names nothing", n)
	}
}

// "#2/3" says: the second of 3 decisions with this text. When the workflow
// has another number of them, the number may point at a different decision,
// so the rule is not applied and a warning says why.
func TestNumberWithCountWarnsWhenTheCountChanges(t *testing.T) {
	g := graph(t, withoutFixtures, twiceWF) // two "if a || b"
	run := func(when, without []string) (*Engine, int) {
		e := ruleEngine(t, scope.Rule{ID: "r", Workflow: twiceWF, Label: "contradictory", When: when, Without: without, Reason: "x"})
		if err := e.AddGraph(twiceWF, g); err != nil {
			t.Fatalf("%v", err)
		}
		n, _ := marked(e, twiceWF, g)
		return e, n
	}
	_, same := run([]string{"if a || b #2 --true"}, nil)
	if same == 0 {
		t.Fatal("the fixture needs paths taking the second decision")
	}
	if e, n := run([]string{"if a || b #2/2 --true"}, nil); n != same || len(e.Findings([]string{twiceWF}, nil)) != 0 {
		t.Errorf("a correct count must behave like the plain number: %d vs %d, %v", n, same, e.Findings([]string{twiceWF}, nil))
	}
	for name, c := range map[string]struct{ when, without []string }{
		"when":    {when: []string{"if a || b #2/3 --true"}},
		"without": {when: []string{"if a || b #1 --true"}, without: []string{"if a || b #2/3 --true"}},
	} {
		e, n := run(c.when, c.without)
		w := strings.Join(e.Findings([]string{twiceWF}, nil), "\n")
		if n != 0 || !strings.Contains(w, "is not applied") || !strings.Contains(w, "had 3 decisions") || !strings.Contains(w, "it has 2 now") || strings.Contains(w, "stale rule") {
			t.Errorf("%s: %d paths marked; warnings: %q", name, n, w)
		}
	}
}

// A numbered entry's anchor is what the map says comes right before the
// decision it names: the start for the first "if a || b", and both ways
// out of the first one for the second. "any:" and unnumbered entries have
// no anchor (they cannot shift).
func TestAnchors(t *testing.T) {
	g := graph(t, withoutFixtures, twiceWF)
	e := ruleEngine(t, scope.Rule{ID: "r", Workflow: twiceWF, Label: "contradictory",
		When: []string{"if a || b #1 --true", "if a || b #2/2 --true"}, Without: []string{"any: if a || b --false"}, Reason: "x"})
	if err := e.AddGraph(twiceWF, g); err != nil {
		t.Fatal(err)
	}
	got := e.Anchors()
	if len(got) != 2 {
		t.Fatalf("anchors: %+v", got)
	}
	if a := got[0]; a.Entry != "if a || b #1 --true" || strings.Join(a.Before, "|") != "Start" || strings.Join(a.After, "|") != "--true--> first (activity)|--false--> if a || b #2" {
		t.Errorf("#1: %+v", a)
	}
	if a := got[1]; a.Entry != "if a || b #2/2 --true" || strings.Join(a.Before, "|") != "first (activity) --success|if a || b --false" || strings.Join(a.After, "|") != "--true--> second (activity)|--false--> End (completed)" {
		t.Errorf("#2: %+v", a)
	}
	if got[0].Count != 2 || got[1].Count != 2 {
		t.Errorf("counts: %d, %d", got[0].Count, got[1].Count)
	}
	// AddGraph again (a second command run on the same engine) does not
	// duplicate them.
	if err := e.AddGraph(twiceWF, g); err != nil {
		t.Fatal(err)
	}
	if len(e.Anchors()) != 2 {
		t.Errorf("anchors duplicated: %+v", e.Anchors())
	}
}
