package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// graph builds one fixture or pilot workflow's graph.
func graph(t *testing.T, folder, name string) *model.Graph {
	t.Helper()
	res, err := load.Load(folder)
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

const (
	billing = "billing.SubscriptionWorkflow"
	polling = "polling.ReportPollingWorkflow"
	pilot   = "../../testdata/pilot/..."
)

// The two pilot paths a person can reasonably call untestable.
const (
	billingNoLoop = "s1:59aa014811bd" // CyclesPerRun is 0
	pollingNoLoop = "s1:c048fc889a4c" // MaxPolls is 0
)

func pairConfig() scope.Classification {
	return scope.Classification{Enabled: true, Overrides: []scope.PathOverride{
		{Workflow: billing, StableID: billingNoLoop, Untestable: true, Label: "manual", Reason: "input count is never 0"},
		{Workflow: polling, StableID: pollingNoLoop, Untestable: true, Label: "manual", Reason: "input count is never 0"},
	}}
}

func classifyAll(e *Engine, name string, g *model.Graph) map[string]Classification {
	out := map[string]Classification{}
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		out[p.StableID()] = e.Classify(name, p)
	}
	return out
}

// The two override examples: exactly these two paths become manual
// cannot-be-tested, with the reason; every other path stays testable.
func TestOverridesByStableID(t *testing.T) {
	e, err := NewEngine(pairConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{billing: billingNoLoop, polling: pollingNoLoop} {
		got := classifyAll(e, name, graph(t, pilot, name))
		for sid, c := range got {
			if sid == id {
				if c.Bucket != CannotBeTested || c.Label != "manual" || c.Source != SourceManual || c.Confidence != Certain || c.PossibleInProduction ||
					!strings.Contains(c.Explanation, "input count is never 0") {
					t.Errorf("%s %s: %+v", name, sid, c)
				}
			} else if c.Bucket != Testable || c.Source != SourceAuto {
				t.Errorf("%s %s should stay testable: %+v", name, sid, c)
			}
		}
	}
	// Overrides are per workflow: the same stableId in another workflow is untouched.
	for _, c := range classifyAll(e, "orders.OrderWorkflow", graph(t, pilot, "orders.OrderWorkflow")) {
		if c.Bucket != Testable {
			t.Errorf("an override leaked into another workflow: %+v", c)
		}
	}
}

func TestNoConfigMeansEverythingTestable(t *testing.T) {
	e, err := NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range classifyAll(e, billing, graph(t, pilot, billing)) {
		if c.Bucket != Testable || c.Source != SourceAuto || len(c.ExtraReasons) != 0 {
			t.Errorf("%+v", c)
		}
	}
	if len(e.Notes(nil, []string{billing})) != 0 || len(e.Findings([]string{billing}, nil)) != 0 {
		t.Errorf("notes %v findings %v", e.Notes(nil, []string{billing}), e.Findings([]string{billing}, nil))
	}
}

const (
	fixtures = "../../testdata/fixtures/untestable"
	checkout = "untestable.Checkout"
	express  = "if express && amount == 0 --true" // Checkout's first decision, true
)

func checkoutPaths(t *testing.T) (*model.Graph, model.Path, model.Path) {
	t.Helper()
	g := graph(t, fixtures, checkout)
	var marked, other model.Path
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if strings.Contains(strings.Join(p.Decisions(), "|"), express) {
			marked = p
		} else {
			other = p
		}
	}
	return g, marked, other
}

// Strongest first: config override > comment > rule. The one that wins is
// the main reason; the others are kept as extra reasons.
func TestPrecedence(t *testing.T) {
	g, marked, other := checkoutPaths(t)
	rule := scope.Rule{ID: "r1", Workflow: checkout, Label: "set-then-test", When: []string{express}, Reason: "rule reason"}

	// comment + rule: the comment wins.
	e, err := NewEngine(scope.Classification{Rules: []scope.Rule{rule}})
	if err != nil {
		t.Fatal(err)
	}
	e.AddMarks(checkout, g.Marks)
	c := e.Classify(checkout, marked)
	if c.Bucket != CannotBeTested || c.Label != "manual" || !strings.Contains(c.Explanation, "//pathkit:untestable at untestable.go:") ||
		len(c.ExtraReasons) != 1 || c.ExtraReasons[0] != `rule "r1"` {
		t.Errorf("comment + rule: %+v", c)
	}
	if c := e.Classify(checkout, other); c.Bucket != Testable {
		t.Errorf("the unmarked path: %+v", c)
	}

	// rule alone.
	e, _ = NewEngine(scope.Classification{Rules: []scope.Rule{rule}})
	c = e.Classify(checkout, marked)
	if c.Label != "set-then-test" || c.RuleID != "r1" || c.Source != SourceManual || !strings.Contains(c.Explanation, "rule reason") || len(c.ExtraReasons) != 0 {
		t.Errorf("rule alone: %+v", c)
	}

	// override beats both; mark_testable beats both and says so.
	for _, untestable := range []bool{true, false} {
		e, _ = NewEngine(scope.Classification{Rules: []scope.Rule{rule}, Overrides: []scope.PathOverride{
			{Workflow: checkout, StableID: marked.StableID(), Untestable: untestable, Label: map[bool]string{true: "dead-code"}[untestable], Reason: "person says so"}}})
		e.AddMarks(checkout, g.Marks)
		c = e.Classify(checkout, marked)
		wantBucket := map[bool]Bucket{true: CannotBeTested, false: Testable}[untestable]
		if c.Bucket != wantBucket || c.Source != SourceManual || !strings.Contains(c.Explanation, "person says so") || len(c.ExtraReasons) != 2 {
			t.Errorf("override (untestable=%v): %+v", untestable, c)
		}
	}
}

// A rule needs every decision of its "when"; spaces don't matter.
func TestRuleNeedsAllDecisions(t *testing.T) {
	g, _, _ := checkoutPaths(t)
	// The path that goes round the first decision and then takes "large".
	var large model.Path
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if strings.Join(p.Decisions(), "|") == "if express && amount == 0 --false|if amount > 100 --true" {
			large = p
		}
	}
	if len(large.Steps) != 2 {
		t.Fatal("the fixture's two-decision path was not found")
	}
	notExpress, big := "if express && amount == 0 --false", "if amount > 100 --true"
	for name, tc := range map[string]struct {
		when []string
		want Bucket
	}{
		"both decisions":         {[]string{notExpress, big}, CannotBeTested},
		"the order is free":      {[]string{big, notExpress}, CannotBeTested},
		"one decision":           {[]string{big}, CannotBeTested},
		"extra spaces":           {[]string{"  if   amount > 100   --true "}, CannotBeTested},
		"a decision it lacks":    {[]string{notExpress, "if amount > 100 --false"}, Testable},
		"the wrong exit":         {[]string{"if amount > 100 --false"}, Testable},
		"text that is not there": {[]string{"if nobody --true"}, Testable},
		"one there, one missing": {[]string{big, "if nobody --true"}, Testable},
	} {
		e, err := NewEngine(scope.Classification{Rules: []scope.Rule{{ID: "r", Workflow: checkout, Label: "set-then-test", When: tc.when, Reason: "why"}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := e.Classify(checkout, large).Bucket; got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
}

func TestFindings(t *testing.T) {
	g := graph(t, pilot, billing)
	_ = g
	cfg := pairConfig()
	cfg.Overrides = append(cfg.Overrides,
		scope.PathOverride{Workflow: billing, StableID: "s1:ffffffffffff", Untestable: true, Label: "manual", Reason: "a typo"},
		scope.PathOverride{Workflow: "shipment.ShipmentWorkflow", StableID: "s1:eeeeeeeeeeee", Untestable: true, Label: "manual", Reason: "not measured now"},
	)
	cfg.Rules = []scope.Rule{{ID: "dead", Workflow: billing, Label: "set-then-test", When: []string{"if never --true"}, Reason: "r"}}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	classifyAll(e, billing, g)
	classifyAll(e, polling, graph(t, pilot, polling))
	got := e.Findings([]string{billing, polling}, nil)
	if len(got) != 2 {
		t.Fatalf("findings %q", got)
	}
	if !strings.Contains(got[0], "stale entry: "+billing+" s1:ffffffffffff (a typo) matches no path") {
		t.Errorf("stale entry message: %q", got[0])
	}
	if !strings.Contains(got[1], `stale rule "dead"`) {
		t.Errorf("stale rule message: %q", got[1])
	}
	// The entry for shipment is not stale: shipment was not measured.
	for _, f := range got {
		if strings.Contains(f, "shipment") {
			t.Errorf("an entry for an unmeasured workflow was called stale: %q", f)
		}
	}
	if len(e.Findings([]string{polling}, nil)) != 0 {
		t.Error("only billing's entries are stale; polling alone should be clean")
	}
}

func TestNotes(t *testing.T) {
	g, marked, other := checkoutPaths(t)
	e, _ := NewEngine(scope.Classification{
		Overrides: []scope.PathOverride{{Workflow: billing, StableID: billingNoLoop, Untestable: true, Label: "manual", Reason: "r"}},
		Rules:     []scope.Rule{{ID: "r1", Workflow: checkout, Label: "set-then-test", When: []string{express}, Reason: "r"}},
	})
	e.AddMarks(checkout, g.Marks)
	final := []Classification{
		e.Classify(billing, firstPath(t, graph(t, pilot, billing), billingNoLoop)),
		e.Classify(checkout, marked), // the comment wins; the rule is an extra reason
		e.Classify(checkout, other),
	}
	want := []string{
		"Config overrides: 1 path marked untestable, 0 paths marked testable",
		"//pathkit:untestable comments: 1 path marked untestable",
		`Rule "r1" (` + checkout + `): 0 paths marked untestable`, // outranked by the comment
	}
	if got := e.Notes(final, []string{billing, checkout}); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("notes\n%q\nwant\n%q", got, want)
	}
	// Only the measured workflows' entries are listed (--function).
	if got := e.Notes(final, []string{checkout}); len(got) != 2 || strings.Contains(strings.Join(got, "|"), "Config overrides") {
		t.Errorf("notes for checkout only: %q", got)
	}
	if got := e.Notes(final, []string{"other.Workflow"}); len(got) != 0 {
		t.Errorf("notes for an unrelated workflow: %q", got)
	}
	// A path a test ran is put back to testable by coverage; it is not "marked".
	final[0] = DefaultTestable()
	if got := e.Notes(final, []string{billing, checkout})[0]; got != "Config overrides: 0 paths marked untestable, 0 paths marked testable" {
		t.Errorf("after the guard: %q", got)
	}
}

func firstPath(t *testing.T, g *model.Graph, stableID string) model.Path {
	t.Helper()
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if p.StableID() == stableID {
			return p
		}
	}
	t.Fatalf("no path %s", stableID)
	return model.Path{}
}

// Everything the config can get wrong about labels is an error that says so.
func TestNewEngineErrors(t *testing.T) {
	over := func(label string) scope.Classification {
		return scope.Classification{Overrides: []scope.PathOverride{{Workflow: "w.W", StableID: "s1:000000000000", Untestable: true, Label: label, Reason: "r"}}}
	}
	rule := func(label string, custom ...scope.CustomLabel) scope.Classification {
		return scope.Classification{Custom: custom, Rules: []scope.Rule{{ID: "r1", Workflow: "w.W", Label: label, When: []string{"x"}, Reason: "r"}}}
	}
	tests := []struct {
		name string
		cfg  scope.Classification
		want string // "" means no error
	}{
		{"an undefined label on an override", over("no-such-label"), `label "no-such-label" is not defined`},
		{"a label from another bucket on an override", over("history-limit"), `puts paths in needs-special-test, not cannot-be-tested`},
		{"a built-in cannot-be-tested label", over("contradictory"), ""},
		{"the default manual label", over(""), ""},
		{"an undefined label on a rule", rule("nope"), `rule "r1": label "nope" is not defined`},
		{"a rule with a needs-review label", rule("model-limit"), `puts paths in needs-review, but a rule says a combination is impossible`},
		{"a rule with a custom label", rule("my-label", scope.CustomLabel{Name: "my-label", Definition: "d", Bucket: "cannot-be-tested"}), ""},
		{"a custom label in the wrong bucket for a rule", rule("my-label", scope.CustomLabel{Name: "my-label", Definition: "d", Bucket: "needs-review"}), `puts paths in needs-review`},
		{"a custom label with a built-in name", scope.Classification{Custom: []scope.CustomLabel{{Name: "contradictory", Definition: "d", Bucket: "cannot-be-tested"}}}, `has the name of a built-in label`},
		{"the same path under two spellings of the workflow", scope.Classification{Overrides: []scope.PathOverride{
			{Workflow: "w.W", StableID: "s1:000000000000", Untestable: true, Label: "manual", Reason: "r"},
			{Workflow: "w.W", StableID: "s1:000000000000", Reason: "r"}}}, "is listed more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEngine(tt.cfg)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestPolicy(t *testing.T) {
	for in, want := range map[string]Policy{"": CountAsTestable, "count_as_testable": CountAsTestable, "exclude": Exclude} {
		e, err := NewEngine(scope.Classification{Policy: in})
		if err != nil || e.Policy() != want {
			t.Errorf("%q: %v %v", in, e.Policy(), err)
		}
	}
}

// A workflow with more paths than the cap: an entry that matches none of
// the LISTED paths may be beyond the cap, so it is not called stale. A
// workflow under the cap still gets "stale".
func TestFindingsBeyondThePathCap(t *testing.T) {
	cfg := scope.Classification{
		Overrides: []scope.PathOverride{{Workflow: billing, StableID: "s1:ffffffffffff", Untestable: true, Label: "manual", Reason: "far away"}},
		Rules:     []scope.Rule{{ID: "far", Workflow: billing, Label: "set-then-test", When: []string{"if nobody --true"}, Reason: "r"}},
	}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	classifyAll(e, billing, graph(t, pilot, billing))

	got := e.Findings([]string{billing}, map[string]bool{billing: true})
	if len(got) != 2 {
		t.Fatalf("findings %q", got)
	}
	for _, f := range got {
		if strings.Contains(f, "stale") || !strings.Contains(f, "beyond the path cap") || !strings.Contains(f, "first 2000") {
			t.Errorf("a truncated workflow's finding must say beyond the path cap, not stale: %q", f)
		}
	}
	for _, f := range e.Findings([]string{billing}, map[string]bool{billing: false}) {
		if !strings.Contains(f, "stale") || strings.Contains(f, "beyond the path cap") {
			t.Errorf("an untruncated workflow's finding must say stale: %q", f)
		}
	}
}
