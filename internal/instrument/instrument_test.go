package instrument_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/instrument"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

const (
	pilot    = "../../testdata/pilot"
	fixtures = "../../testdata/fixtures"
)

// targets loads arg and returns every workflow M2 can map, with its graph.
func targets(t *testing.T, arg string) []instrument.Target {
	t.Helper()
	res, err := load.Load(arg)
	if err != nil {
		t.Fatal(err)
	}
	var out []instrument.Target
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.Build(wf)
		var u *model.UnsupportedError
		if errors.As(err, &u) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, instrument.Target{Workflow: wf, Graph: g})
	}
	return out
}

// allTargets is every mappable workflow in the fixtures and the pilot.
func allTargets(t *testing.T) []instrument.Target {
	t.Helper()
	var out []instrument.Target
	for _, arg := range []string{fixtures + "/rules", fixtures + "/switches", fixtures + "/loops", pilot + "/..."} {
		out = append(out, targets(t, arg)...)
	}
	return out
}

func instrumentAll(t *testing.T, ts []instrument.Target) *instrument.Result {
	t.Helper()
	res, err := instrument.Instrument(ts, "/tmp/pathkit-test-traces")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The marked-up copies must compile, in both modules.
func TestInstrumentedCopiesCompile(t *testing.T) {
	cases := []struct{ dir, arg string }{
		{fixtures, fixtures + "/rules"},
		{fixtures, fixtures + "/panics"},
		{fixtures, fixtures + "/replay"},
		{fixtures, fixtures + "/switches"},
		{fixtures, fixtures + "/loops"},
		{pilot, pilot + "/..."},
	}
	for _, c := range cases {
		ts := targets(t, c.arg)
		if len(ts) == 0 {
			t.Fatalf("%s: nothing to instrument", c.arg)
		}
		overlay, err := instrument.WriteOverlay(instrumentAll(t, ts), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		pattern := "./" + strings.TrimPrefix(strings.TrimPrefix(c.arg, c.dir), "/")
		if strings.HasSuffix(c.arg, "/...") {
			pattern = "./..."
		}
		cmd := exec.Command("go", "vet", "-overlay="+overlay, pattern)
		cmd.Dir = c.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: instrumented copy does not compile/vet: %v\n%s", c.arg, err, out)
		}
	}
}

// No line break is ever inserted, so every line keeps its number.
func TestLineNumbersUnchanged(t *testing.T) {
	ts := allTargets(t)
	res := instrumentAll(t, ts)
	for path, edits := range res.Edits {
		orig, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range edits {
			if strings.ContainsAny(e.Text, "\r\n") {
				t.Errorf("%s: edit %q contains a line break", path, e.Text)
			}
		}
		if a, b := bytes.Count(orig, []byte("\n")), bytes.Count(res.Files[path], []byte("\n")); a != b {
			t.Errorf("%s: %d lines became %d", path, a, b)
		}
	}
}

// Every exit has exactly one hit() in the copy, every hit() ID is a real
// exit (via LookupEdge), there is one pathkitStart, and every return of
// the workflow function is marked.
func TestEveryExitRecordedOnce(t *testing.T) {
	ts := allTargets(t)
	res := instrumentAll(t, ts)
	for _, tg := range ts {
		fn := findFunc(t, res.Files[tg.Workflow.Filename], tg.Workflow.Func)
		var hits []string
		starts, marks := 0, 0
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callName(call) {
			case "pathkitRec.hit", "pathkitRec.hitUnlessFell":
				id, _ := strconv.Unquote(call.Args[0].(*ast.BasicLit).Value)
				hits = append(hits, id)
			case "pathkitRec.enter": // a loop's iterate, exit ("" for "for {}") and retry IDs
				for _, arg := range call.Args[1:] {
					if id, _ := strconv.Unquote(arg.(*ast.BasicLit).Value); id != "" {
						hits = append(hits, id)
					}
				}
			case "pathkitStart":
				starts++
			case "pathkitRec.returned":
				marks++
			default:
				if strings.HasPrefix(callName(call), "pathkitRet") {
					marks++
				}
			}
			return true
		})

		var want []string
		for _, j := range tg.Graph.Junctions {
			for _, e := range j.Exits {
				want = append(want, e.ID.String())
			}
			if j.Retry != nil {
				want = append(want, j.Retry.ID.String())
			}
		}
		slices.Sort(hits)
		slices.Sort(want)
		if !slices.Equal(hits, want) {
			t.Errorf("%s: hit() IDs %v, want exactly the exits %v", tg.Workflow.Name, hits, want)
		}
		for _, id := range hits {
			if _, ok := tg.Graph.LookupEdge(id); !ok {
				t.Errorf("%s: hit(%q) is not an exit known to the model", tg.Workflow.Name, id)
			}
		}
		if starts != 1 {
			t.Errorf("%s: %d pathkitStart calls, want 1", tg.Workflow.Name, starts)
		}
		if want := countReturns(tg.Workflow.Func); marks != want {
			t.Errorf("%s: %d returns marked, want %d", tg.Workflow.Name, marks, want)
		}
	}
}

func TestNameClashes(t *testing.T) {
	for arg, want := range map[string]string{
		fixtures + "/clash":    `the package already declares "pathkitStart"`,
		fixtures + "/clashvar": `it already uses the name "pathkitRec"`,
	} {
		_, err := instrument.Instrument(targets(t, arg), "/tmp/x")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to mention %s", arg, err, want)
		}
	}
}

func TestOverlayJSON(t *testing.T) {
	dir := t.TempDir()
	res := instrumentAll(t, targets(t, pilot+"/orders"))
	overlay, err := instrument.WriteOverlay(res, filepath.Join(dir, "overlay"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(overlay)
	orders, _ := filepath.Abs(pilot + "/orders/orders.go")
	recorder, _ := filepath.Abs(pilot + "/orders/" + instrument.RecorderFile)
	for _, want := range []string{orders, recorder} {
		if !strings.Contains(string(data), strconv.Quote(want)) {
			t.Errorf("overlay.json does not replace %s:\n%s", want, data)
		}
	}
	if _, err := os.Stat(recorder); err == nil {
		t.Errorf("the recorder file must not exist in the project: %s", recorder)
	}
}

func findFunc(t *testing.T, src []byte, orig *ast.FuncDecl) *ast.FuncDecl {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("instrumented source does not parse: %v", err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == orig.Name.Name && (fn.Recv == nil) == (orig.Recv == nil) {
			return fn
		}
	}
	t.Fatalf("function %s not found in instrumented source", orig.Name.Name)
	return nil
}

func callName(call *ast.CallExpr) string {
	fun := call.Fun
	if ix, ok := fun.(*ast.IndexListExpr); ok { // pathkitRet2[string, error](...)
		fun = ix.X
	}
	if ix, ok := fun.(*ast.IndexExpr); ok { // pathkitRet1[error](...)
		fun = ix.X
	}
	switch fun := fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			return x.Name + "." + fun.Sel.Name
		}
	}
	return ""
}

func countReturns(fn *ast.FuncDecl) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			n++
		}
		return true
	})
	return n
}
