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
	for _, name := range m2Workflows {
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
