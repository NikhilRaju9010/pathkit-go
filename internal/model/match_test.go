package model_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func pilotGraph(t *testing.T, name string) *model.Graph {
	t.Helper()
	g, err := model.Build(workflowsIn(t, "../../testdata/pilot/...")[name])
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// Every listed path, replayed as a trace, must land on itself.
func TestMatchEveryListedPath(t *testing.T) {
	for _, name := range keyWorkflows(t) {
		g := pilotGraph(t, name)
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			var steps []string
			for _, s := range p.Steps {
				steps = append(steps, s.Exit.ID.String())
			}
			got, mm := g.Match(steps)
			if mm != nil || got.Key() != p.Key() {
				t.Errorf("%s: Match(%v) = %q, %v; want %q", name, steps, got.Key(), mm, p.Key())
			}
		}
	}
}

func TestMatchMismatches(t *testing.T) {
	g := pilotGraph(t, "orders.OrderWorkflow")
	tests := []struct {
		steps []string
		step  int
		want  string
	}{
		{[]string{"J9.true"}, 1, `step 1 "J9.true" is not a junction exit in orders.OrderWorkflow`},
		{[]string{"J2.failure"}, 1, `step 1 "J2.failure" does not fit: the path is at J1 (if in.AmountCents <= 0)`},
		{[]string{"J1.true", "J2.success"}, 2, `step 2 "J2.success" comes after the path already ended at End (completed)`},
		{[]string{"J1.false"}, 0, `the trace stops at J2 (ChargeCard (activity)) before reaching an end`},
		{nil, 0, `the trace stops at J1 (if in.AmountCents <= 0) before reaching an end`},
	}
	for _, tt := range tests {
		_, mm := g.Match(tt.steps)
		if mm == nil || mm.Step != tt.step || mm.Reason != tt.want {
			t.Errorf("Match(%v) = %+v, want step %d %q", tt.steps, mm, tt.step, tt.want)
		}
	}
}

func parseFunc(t *testing.T, src string) (*token.FileSet, *ast.FuncDecl) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return fset, f.Decls[0].(*ast.FuncDecl)
}

func TestFunctionHash(t *testing.T) {
	base := "package p\n\n// W does things.\nfunc W(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	reformatted := "package p\nfunc W(x int) int { // a comment\n  if x > 0 { return 1 }\n\n\n  /* more */ return 0 }\n"
	changed := strings.Replace(base, "x > 0", "x >= 0", 1)

	h := func(src string) string { return model.FunctionHash(parseFunc(t, src)) }
	if len(h(base)) != 16 {
		t.Fatalf("hash %q is not 16 hex digits", h(base))
	}
	if h(base) != h(reformatted) {
		t.Errorf("comments/formatting changed the hash: %s vs %s", h(base), h(reformatted))
	}
	if h(base) == h(changed) {
		t.Errorf("a real code change did not change the hash")
	}
}

// repeat returns steps written n times.
func repeat(n int, steps ...string) []string {
	var out []string
	for range n {
		out = append(out, steps...)
	}
	return out
}

func concat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// The matcher half of the loop rule (CLAUDE.md D3): a trace that goes
// round a loop several times is folded by keeping only the last trip.
func TestMatchFoldsLoopTrips(t *testing.T) {
	pending := []string{"J1.iterate", "J2.success", "J3.default", "J1.retry"}
	const polling = "pilot:polling.ReportPollingWorkflow"

	tests := []struct {
		name     string
		workflow string // a rules fixture, or "pilot:<name>"
		steps    []string
		want     string
	}{
		{"zero trips (path a)", "LoopWithActivity",
			[]string{"J1.exit"}, "J1.exit|completed"},
		{"several trips, then return from inside the body (path b)", "LoopWithActivity",
			concat(repeat(3, "J1.iterate", "J2.success", "J1.retry"), []string{"J1.iterate", "J2.failure"}),
			"J1.iterate J2.failure|failed"},
		{"one trip, then left (path c)", "LoopWithActivity",
			[]string{"J1.iterate", "J2.success", "J1.retry", "J1.exit"},
			"J1.iterate J2.success J1.retry J1.exit|completed"},
		{"many trips, then left (path c)", "LoopWithActivity",
			concat(repeat(5, "J1.iterate", "J2.success", "J1.retry"), []string{"J1.exit"}),
			"J1.iterate J2.success J1.retry J1.exit|completed"},
		{"trips with different choices keep only the last", "ContinueInLoop",
			concat([]string{"J1.iterate", "J2.true", "J1.retry", "J1.iterate", "J2.false", "J3.success", "J1.retry", "J1.iterate", "J2.true", "J1.retry"}, []string{"J1.exit"}),
			"J1.iterate J2.true J1.retry J1.exit|completed"},
		{"for {} left by break after several trips", "ForeverWithBreak",
			concat(repeat(4, "J1.iterate", "J2.success", "J3.false", "J1.retry"), []string{"J1.iterate", "J2.success", "J3.true"}),
			"J1.iterate J2.success J3.true|completed"},
		{"nested: inner goes round twice on each of three outer trips", "NestedLoops",
			concat(repeat(3, "J1.iterate", "J2.iterate", "J3.success", "J2.retry", "J2.iterate", "J3.success", "J2.retry", "J2.exit", "J1.retry"), []string{"J1.exit"}),
			"J1.iterate J2.iterate J3.success J2.retry J2.exit J1.retry J1.exit|completed"},
		{"nested: fails on the second inner trip of the second outer trip", "NestedLoops",
			[]string{"J1.iterate", "J2.iterate", "J3.success", "J2.retry", "J2.iterate", "J3.success", "J2.retry", "J2.exit", "J1.retry",
				"J1.iterate", "J2.iterate", "J3.success", "J2.retry", "J2.iterate", "J3.failure"},
			"J1.iterate J2.iterate J3.failure|failed"},
		{"nested: inner loop not entered on the last outer trip", "NestedLoops",
			[]string{"J1.iterate", "J2.iterate", "J3.success", "J2.retry", "J2.exit", "J1.retry", "J1.iterate", "J2.exit", "J1.retry", "J1.exit"},
			"J1.iterate J2.exit J1.retry J1.exit|completed"},
		{"labeled continue outer, then a full row", "LabeledBreakContinue",
			[]string{"J1.iterate", "J2.iterate", "J3.false", "J4.false", "J5.success", "J2.retry", "J2.iterate", "J3.true", "J1.retry",
				"J1.iterate", "J2.iterate", "J3.false", "J4.false", "J5.success", "J2.retry", "J2.exit", "J1.retry", "J1.exit"},
			"J1.iterate J2.iterate J3.false J4.false J5.success J2.retry J2.exit J1.retry J1.exit|completed"},
		{"labeled break outer after a trip", "LabeledBreakContinue",
			[]string{"J1.iterate", "J2.iterate", "J3.false", "J4.false", "J5.success", "J2.retry", "J2.iterate", "J3.false", "J4.true"},
			"J1.iterate J2.iterate J3.false J4.true|completed"},
		{"pilot: pending, then complete (EXPECTED path 3)", polling,
			concat(pending, []string{"J1.iterate", "J2.success", `J3.case "complete"`}),
			`J1.iterate J2.success J3.case "complete"|completed`},
		{"pilot: gives up after 3 polls (EXPECTED path 5)", polling,
			concat(repeat(3, pending...), []string{"J1.exit"}),
			"J1.iterate J2.success J3.default J1.retry J1.exit|completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g *model.Graph
			if name, ok := strings.CutPrefix(tt.workflow, "pilot:"); ok {
				g = pilotGraph(t, name)
			} else {
				var err error
				if g, err = buildRule(t, tt.workflow); err != nil {
					t.Fatal(err)
				}
			}
			got, mm := g.Match(tt.steps)
			if mm != nil || got.Key() != tt.want {
				t.Fatalf("Match(%v) = %q, %v; want %q", tt.steps, got.Key(), mm, tt.want)
			}
			if !listed(g, got) {
				t.Errorf("folded path %q is not one of the listed paths", got.Key())
			}
		})
	}
}

func TestMatchLoopMismatches(t *testing.T) {
	g, err := buildRule(t, "LoopWithActivity")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		steps []string
		step  int
		want  string
	}{
		{[]string{"J1.iterate", "J2.success", "J1.exit"}, 3,
			`step 3 "J1.exit" does not fit: the path goes round loop J1 (for i < n) next, so J1.retry comes first`},
		{[]string{"J1.iterate", "J2.success", "J1.retry", "J2.success"}, 4,
			`step 4 "J2.success" does not fit: the path is at J1 (for i < n)`},
		{[]string{"J1.retry"}, 1,
			`step 1 "J1.retry" does not fit: the path is at J1 (for i < n)`},
		{[]string{"J1.iterate", "J2.success"}, 0,
			`the trace stops before J1.retry (going round loop J1) and never reaches an end`},
	}
	for _, tt := range tests {
		_, mm := g.Match(tt.steps)
		if mm == nil || mm.Step != tt.step || mm.Reason != tt.want {
			t.Errorf("Match(%v) = %+v, want step %d %q", tt.steps, mm, tt.step, tt.want)
		}
	}
}

func listed(g *model.Graph, p model.Path) bool {
	for _, q := range g.Paths(model.DefaultMaxPaths).List {
		if q.Key() == p.Key() {
			return true
		}
	}
	return false
}
