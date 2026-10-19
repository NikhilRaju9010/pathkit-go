// Package scaffold writes a Go test skeleton for one scenario of a workflow
// (docs/DESIGN.md D29): the test environment, the workflow registered, the
// activities mocked with the outcomes the scenario needs, delayed signals and
// cancels, an input that takes the decisions the scenario takes (when PathKit
// can work it out), and the assertions. What it cannot work out is left as a
// clear TODO line; it never guesses silently.
package scaffold

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

const workflowPkg = "go.temporal.io/sdk/workflow"

// Test is the scaffold of one scenario: the imports it needs and the test
// function.
type Test struct {
	Name     string
	Workflow string // as PathKit names it, "orders.OrderWorkflow"
	Func     string // the test function's source
	Imports  map[string]string
	Todos    []string
	Package  string // the Go package name of the workflow's package
}

// File is the name the tests of one workflow are written to.
func File(workflow string) string {
	name := workflow
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return "pathkit_scaffold_" + b.String() + "_test.go"
}

// fixedImportNames are the imports the generated code names itself.
var fixedImportNames = map[string]bool{"testing": true, "time": true, "errors": true, "go.temporal.io/sdk/testsuite": true,
	"github.com/stretchr/testify/mock": true, "go.temporal.io/sdk/temporal": true, "go.temporal.io/sdk/workflow": true}

type builder struct {
	g       *model.Graph
	info    *types.Info
	imports map[string]string // import path -> name used in the file
	used    map[string]bool   // names taken
	todos   []string
}

func (b *builder) todo(format string, args ...any) string {
	s := fmt.Sprintf(format, args...)
	b.todos = append(b.todos, s)
	return "// TODO: " + s
}

// pkgName returns the name to use for an import path, adding the import.
func (b *builder) pkgName(path, want string) string {
	if n, ok := b.imports[path]; ok {
		return n
	}
	name := want
	if !fixedImportNames[path] {
		for b.used[name] {
			name += "x"
		}
	}
	b.used[name] = true
	b.imports[path] = name
	return name
}

// qualifier writes the types of other packages with the import's name.
func (b *builder) qualifier(p *types.Package) string {
	if p == b.g.Types() {
		return ""
	}
	return b.pkgName(p.Path(), p.Name())
}

func (b *builder) typeString(t types.Type) string { return types.TypeString(t, b.qualifier) }

// zero is an expression for the zero value of t.
func (b *builder) zero(t types.Type) string {
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return "nil"
	}
	return "*new(" + b.typeString(t) + ")"
}

// Generate writes the scaffold of one item: the main listed path of its
// scenario (or group) is the road the test must make the run take.
func Generate(item advice.Item, g *model.Graph, path model.Path) (Test, error) {
	fn := g.Func()
	if fn == nil || fn.Type == nil || g.TypesInfo() == nil {
		return Test{}, fmt.Errorf("%s: PathKit has no code to write a test from", item.Workflow)
	}
	// names the test's own code uses: no parameter may take them, and no import
	// may be named like a parameter
	reserved := map[string]bool{"testing": true, "t": true, "env": true, "suite": true, "err": true, "result": true, "errors": true, "time": true, "mock": true, "temporal": true, "testsuite": true, "again": true}
	b := &builder{g: g, info: g.TypesInfo(), imports: map[string]string{}, used: reserved}
	var out bytes.Buffer
	w := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }

	// The workflow's parameters after the context: one variable each.
	type param struct {
		name string
		typ  types.Type
		obj  types.Object
	}
	var params []param
	idx := 0
	for _, f := range fn.Type.Params.List {
		names := f.Names
		if len(names) == 0 {
			names = []*ast.Ident{nil}
		}
		for _, n := range names {
			if idx > 0 {
				p := param{name: fmt.Sprintf("arg%d", idx), typ: b.info.TypeOf(f.Type)}
				if n != nil && n.Name != "_" {
					p.name = n.Name
					p.obj = b.info.Defs[n]
				}
				for reserved[p.name] || p.name == "workflow" {
					p.name += "Arg"
				}
				reserved[p.name] = true
				params = append(params, p)
			}
			idx++
		}
	}

	// What the test must set up, in the order the path takes it.
	steps := g.Fingerprint(path)
	cancel := false
	for _, s := range steps {
		if s == "cancel context" || strings.Contains(s, "failure kind: cancelled") || strings.Contains(s, "(timer): failure") || strings.Contains(s, "(wait): failure") {
			cancel = true
		}
	}
	assigns, unsolved := b.solveInputs(path, params2map(params, func(p param) types.Object { return p.obj }))
	mocks, signals, notes := b.mocks(path)

	// The function.
	w("// %s runs %s through the scenario %s:", item.TestName, item.Workflow, scenarioNote(item))
	w("// %s.", item.Hint)
	w("// Written by pathkit scaffold; edit it freely. The TODO lines are what PathKit could not work out.")
	w("func %s(t *testing.T) {", item.TestName)
	w("\tvar suite testsuite.WorkflowTestSuite")
	w("\tenv := suite.NewTestWorkflowEnvironment()")
	b.pkgName("go.temporal.io/sdk/testsuite", "testsuite")
	if len(params) > 0 {
		w("")
		w("\t// The input. TODO: fill in what the workflow needs beyond the lines that take the decisions below.")
		for _, p := range params {
			decl := b.zero(p.typ)
			if ptr, ok := p.typ.Underlying().(*types.Pointer); ok {
				decl = "new(" + b.typeString(ptr.Elem()) + ")"
			}
			w("\t%s := %s", p.name, decl)
		}
		for _, a := range assigns {
			w("\t%s = %s", a.lhs, a.rhs)
		}
		for _, u := range unsolved {
			w("\t%s", b.todo("%s", u))
		}
		for _, p := range params {
			w("\t_ = %s", p.name)
		}
	}
	if len(mocks) > 0 {
		w("")
		w("\t// The calls the scenario makes, with the outcomes it needs.")
		for _, m := range mocks {
			w("\t%s", m)
		}
	}
	for _, n := range notes {
		w("\t%s", n)
	}
	delay := 0
	if len(signals) > 0 || cancel {
		w("")
	}
	for _, s := range signals {
		delay++
		b.pkgName("time", "time")
		w("\tenv.RegisterDelayedCallback(func() { env.SignalWorkflow(%s, nil) }, %d*time.Millisecond) %s", strconv.Quote(s), delay, b.todo("send the payload the signal %q carries instead of nil", s))
	}
	if cancel {
		delay++
		b.pkgName("time", "time")
		w("\tenv.RegisterDelayedCallback(func() { env.CancelWorkflow() }, %d*time.Millisecond)", delay+5)
	}
	w("")
	call := b.workflowRef(fn)
	args := make([]string, 0, len(params))
	for _, p := range params {
		args = append(args, p.name)
	}
	w("\tenv.ExecuteWorkflow(%s)", strings.Join(append([]string{call}, args...), ", "))
	w("")
	w("\tif !env.IsWorkflowCompleted() {")
	w("\t\tt.Fatal(\"the workflow did not complete\")")
	w("\t}")
	w("\terr := env.GetWorkflowError()")
	switch path.End {
	case model.EndFailed:
		w("\tif err == nil {")
		w("\t\tt.Fatal(\"expected the workflow to fail, but it completed\")")
		w("\t}")
		w("\t%s", b.todo("check the error is the one you expect (errors.As to *temporal.ApplicationError, temporal.IsCanceledError ...)"))
		b.pkgName("go.temporal.io/sdk/temporal", "temporal")
		w("\t_ = temporal.IsCanceledError")
	case model.EndContinuedAsNew:
		b.pkgName("errors", "errors")
		w("\tvar again *workflow.ContinueAsNewError")
		w("\tif !errors.As(err, &again) {")
		w("\t\tt.Fatalf(\"expected the workflow to continue as new, got %%v\", err)")
		w("\t}")
		b.pkgName(workflowPkg, "workflow")
	default:
		w("\tif err != nil {")
		w("\t\tt.Fatalf(\"the workflow failed: %%v\", err)")
		w("\t}")
	}
	if rt := resultType(b.info, fn); rt != nil {
		w("\tvar result %s", b.typeString(rt))
		w("\tif err == nil {")
		w("\t\t_ = env.GetWorkflowResult(&result)")
		w("\t}")
		w("\t%s", b.todo("check the result"))
		w("\t_ = result")
	} else {
		w("\t%s", b.todo("check what the workflow did (the calls it made, its result)"))
	}
	w("}")

	// Imports used by the generated lines.
	src := out.String()
	if strings.Contains(src, "mock.") {
		b.pkgName("github.com/stretchr/testify/mock", "mock")
	}
	return Test{Name: item.TestName, Workflow: item.Workflow, Func: src, Imports: b.imports, Todos: b.todos, Package: g.Types().Name()}, nil
}

func scenarioNote(it advice.Item) string {
	if strings.HasPrefix(it.ID, "sc:") {
		return it.ID
	}
	return "(a group of paths: " + it.ID + ")"
}

func params2map[T any](ps []T, get func(T) types.Object) map[types.Object]bool {
	m := map[types.Object]bool{}
	for _, p := range ps {
		if o := get(p); o != nil {
			m[o] = true
		}
	}
	return m
}

// workflowRef is how the workflow function is named in ExecuteWorkflow.
func (b *builder) workflowRef(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := b.info.TypeOf(fn.Recv.List[0].Type)
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	return "new(" + b.typeString(t) + ")." + fn.Name.Name
}

func resultType(info *types.Info, fn *ast.FuncDecl) types.Type {
	if fn.Type.Results == nil {
		return nil
	}
	var ts []types.Type
	for _, f := range fn.Type.Results.List {
		n := max(len(f.Names), 1)
		for range n {
			ts = append(ts, info.TypeOf(f.Type))
		}
	}
	if len(ts) < 2 {
		return nil
	}
	return ts[0]
}

// ---- mocks and signals ----

type entry struct {
	kind       string // "activity" or "child"
	fn         string // the expression naming the function
	n          int    // arguments after the context
	rets       []types.Type
	fail       bool
	kindOfFail string
}

// mocks reads the path's checked calls (their outcomes) and signals.
func (b *builder) mocks(path model.Path) (lines, signals, notes []string) {
	var entries []entry
	for i, s := range path.Steps {
		j, e := s.Junction, s.Exit
		switch {
		case j.Kind == model.Selector && strings.HasPrefix(e.Label, "signal "):
			if name, err := strconv.Unquote(strings.TrimPrefix(e.Label, "signal ")); err == nil {
				signals = append(signals, name)
			} else {
				notes = append(notes, b.todo("send the signal %s before the timeout", e.Label))
			}
		case j.Kind == model.ErrCheck && j.CallExpr() != nil && !isHelperResult(j):
			call := j.CallExpr()
			if !strings.Contains(j.Label, "(activity)") && !strings.Contains(j.Label, "(local activity)") && !strings.Contains(j.Label, "(child workflow)") {
				continue // a timer or a wait: nothing to mock
			}
			ent, ok, why := b.entryFor(call)
			if !ok {
				notes = append(notes, b.todo("mock %s so that it %s: %s", j.Label, map[bool]string{true: "fails", false: "succeeds"}[e.Label == "failure"], why))
				continue
			}
			ent.fail = e.Label == "failure"
			if ent.fail {
				for k := i + 1; k < len(path.Steps) && k <= i+4; k++ {
					if kind, ok := b.g.FailureKindOf(path.Steps[k].Exit); ok {
						ent.kindOfFail = kind
						break
					}
				}
			}
			entries = append(entries, ent)
		}
	}
	count := map[string]int{}
	for _, e := range entries {
		count[e.kind+e.fn]++
	}
	for _, e := range entries {
		lines = append(lines, b.mockLine(e, count[e.kind+e.fn] > 1))
	}
	return lines, signals, notes
}

func isHelperResult(j *model.Junction) bool { return strings.Contains(j.Label, ", via ") }

// entryFor resolves an ExecuteActivity / ExecuteLocalActivity /
// ExecuteChildWorkflow call to the function it starts.
func (b *builder) entryFor(call *ast.CallExpr) (entry, bool, string) {
	var f *types.Func
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		f, _ = b.info.Uses[fun].(*types.Func)
	case *ast.SelectorExpr:
		f, _ = b.info.Uses[fun.Sel].(*types.Func)
	}
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != workflowPkg {
		return entry{}, false, "this call is not an ExecuteActivity or ExecuteChildWorkflow PathKit can write a mock for"
	}
	var kind string
	switch f.Name() {
	case "ExecuteActivity", "ExecuteLocalActivity":
		kind = "activity"
	case "ExecuteChildWorkflow":
		kind = "child"
	default:
		return entry{}, false, "only activities and child workflows are mocked here"
	}
	if len(call.Args) < 2 || call.Ellipsis != token.NoPos {
		return entry{}, false, "the call has no function to mock"
	}
	target := ast.Unparen(call.Args[1])
	var obj types.Object
	var expr string
	var pkgImport *types.Package
	switch t := target.(type) {
	case *ast.Ident:
		obj = b.info.Uses[t]
		expr = t.Name
	case *ast.SelectorExpr:
		obj = b.info.Uses[t.Sel]
		if pkgID, ok := t.X.(*ast.Ident); ok {
			if pn, isPkg := b.info.Uses[pkgID].(*types.PkgName); isPkg {
				pkgImport = pn.Imported()
				expr = "\x00." + t.Sel.Name
			}
		}
	default:
		return entry{}, false, "the activity is named by a value, not a function"
	}
	fn, isFn := obj.(*types.Func)
	if !isFn || expr == "" {
		return entry{}, false, "the activity is a method or is named by a text, so PathKit cannot reference it here; mock it by its registered name"
	}
	sig := fn.Type().(*types.Signature)
	if sig.Recv() != nil || sig.Variadic() {
		return entry{}, false, "the activity is a method or takes a variable number of arguments"
	}
	if pkgImport != nil { // the import is only needed now that the mock will be written
		expr = b.pkgName(pkgImport.Path(), pkgImport.Name()) + strings.TrimPrefix(expr, "\x00")
	}
	e := entry{kind: kind, fn: expr, n: len(call.Args) - 2}
	res := sig.Results()
	for i := 0; i < res.Len(); i++ {
		if types.Identical(res.At(i).Type(), types.Universe.Lookup("error").Type()) {
			continue
		}
		e.rets = append(e.rets, res.At(i).Type())
	}
	return e, true, ""
}

func (b *builder) mockLine(e entry, once bool) string {
	method := "OnActivity"
	if e.kind == "child" {
		method = "OnWorkflow"
	}
	anys := make([]string, 0, e.n+2)
	anys = append(anys, e.fn)
	for range e.n + 1 { // the context and the arguments
		anys = append(anys, "mock.Anything")
	}
	var rets []string
	for _, t := range e.rets {
		rets = append(rets, b.zero(t))
	}
	if e.fail {
		rets = append(rets, b.failure(e.kindOfFail))
	} else {
		rets = append(rets, "nil")
	}
	line := fmt.Sprintf("env.%s(%s).Return(%s)", method, strings.Join(anys, ", "), strings.Join(rets, ", "))
	if once {
		line += ".Once()"
	}
	if e.fail && e.kindOfFail == "" {
		line += " " + b.todo("if the activity retries, make sure its retry policy lets this failure through")
	}
	return line
}

// failure writes the error a failed call returns, of the kind the scenario
// tells apart.
func (b *builder) failure(kind string) string {
	t := b.pkgName("go.temporal.io/sdk/temporal", "temporal")
	switch kind {
	case "timeout":
		enums := b.pkgName("go.temporal.io/api/enums/v1", "enumspb")
		return fmt.Sprintf("%s.NewTimeoutError(%s.TIMEOUT_TYPE_START_TO_CLOSE, nil)", t, enums)
	case "cancelled":
		return t + ".NewCanceledError()"
	case "application error":
		return t + ".NewApplicationError(\"scaffold: the call failed\", \"ScaffoldError\")"
	}
	// a plain failure must not be retried for ever by a default retry policy
	return t + ".NewNonRetryableApplicationError(\"scaffold: the call failed\", \"ScaffoldError\", nil)"
}

// ---- inputs ----

type assign struct{ lhs, rhs string }

// solveInputs works out assignments to the workflow's parameters that make the
// decisions of the path go the way the path goes. A decision it cannot solve
// is returned as text for a TODO line.
func (b *builder) solveInputs(path model.Path, params map[types.Object]bool) ([]assign, []string) {
	var out []assign
	var unsolved []string
	have := map[string]string{}
	for _, s := range path.Steps {
		j := s.Junction
		if j.Kind != model.PlainIf {
			continue
		}
		ifs, ok := j.Stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		want := s.Exit.Label == "true"
		as, err := b.solve(ifs.Cond, want, params)
		if err != "" {
			unsolved = append(unsolved, fmt.Sprintf("make `%s` %s by setting the input (%s)", strings.TrimPrefix(j.Label, "if "), s.Exit.Label, err))
			continue
		}
		for _, a := range as {
			if prev, dup := have[a.lhs]; dup {
				if prev != a.rhs {
					unsolved = append(unsolved, fmt.Sprintf("make `%s` %s: it needs %s = %s, but an earlier decision needs %s", strings.TrimPrefix(j.Label, "if "), s.Exit.Label, a.lhs, a.rhs, prev))
				}
				continue
			}
			have[a.lhs] = a.rhs
			out = append(out, a)
		}
	}
	return out, unsolved
}

// solve returns assignments that make cond evaluate to want.
func (b *builder) solve(cond ast.Expr, want bool, params map[types.Object]bool) ([]assign, string) {
	cond = ast.Unparen(cond)
	switch x := cond.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return b.solve(x.X, !want, params)
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			// both sides when the whole is true for &&, or false for ||
			both := (x.Op == token.LAND) == want
			l, why := b.solve(x.X, want, params)
			if !both {
				if why == "" {
					return l, ""
				}
				return b.solve(x.Y, want, params) // one side is enough
			}
			if why != "" {
				return nil, why
			}
			r, why := b.solve(x.Y, want, params)
			if why != "" {
				return nil, why
			}
			return append(l, r...), ""
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return b.solveCompare(x, want, params)
		}
	}
	if tv, ok := b.info.Types[cond]; ok && isBool(tv.Type) {
		lhs, why := b.target(cond, params)
		if why != "" {
			return nil, why
		}
		return []assign{{lhs, strconv.FormatBool(want)}}, ""
	}
	return nil, "PathKit does not solve this kind of condition"
}

func isBool(t types.Type) bool {
	bt, ok := t.Underlying().(*types.Basic)
	return ok && bt.Info()&types.IsBoolean != 0
}

// target writes the assignable expression of a parameter or a field of it.
func (b *builder) target(e ast.Expr, params map[types.Object]bool) (string, string) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		o := b.info.Uses[x]
		if o == nil || !params[o] {
			return "", "it does not read the workflow's input directly"
		}
		return x.Name, ""
	case *ast.SelectorExpr:
		if sel := b.info.Selections[x]; sel == nil || sel.Kind() != types.FieldVal {
			return "", "it reads a method or a package value"
		}
		root, why := b.target(x.X, params)
		if why != "" {
			return "", why
		}
		if _, isRoot := ast.Unparen(x.X).(*ast.Ident); !isRoot {
			if _, isPtr := b.info.TypeOf(x.X).Underlying().(*types.Pointer); isPtr {
				return "", "it goes through a pointer field that the test would have to create first"
			}
		}
		return root + "." + x.Sel.Name, ""
	}
	return "", "it reads something other than a field of the input"
}

func (b *builder) solveCompare(x *ast.BinaryExpr, want bool, params map[types.Object]bool) ([]assign, string) {
	left, right, op := x.X, x.Y, x.Op
	if tv, ok := b.info.Types[left]; ok && tv.Value != nil { // constant on the left: swap
		left, right = right, left
		switch op {
		case token.LSS:
			op = token.GTR
		case token.LEQ:
			op = token.GEQ
		case token.GTR:
			op = token.LSS
		case token.GEQ:
			op = token.LEQ
		}
	}
	tv, ok := b.info.Types[right]
	if !ok || tv.Value == nil {
		return nil, "it compares with something that is not a constant"
	}
	lhs, why := b.target(left, params)
	if why != "" {
		return nil, why
	}
	if !want { // not (a op c) is (a inverse-op c)
		switch op {
		case token.EQL:
			op = token.NEQ
		case token.NEQ:
			op = token.EQL
		case token.LSS:
			op = token.GEQ
		case token.LEQ:
			op = token.GTR
		case token.GTR:
			op = token.LEQ
		case token.GEQ:
			op = token.LSS
		}
	}
	lt := b.info.TypeOf(left)
	bt, isBasic := lt.Underlying().(*types.Basic)
	if !isBasic {
		return nil, "the value is not a number, a text or a bool"
	}
	typeName := b.typeString(lt)
	conv := func(lit string) string {
		if _, named := lt.(*types.Named); named {
			return typeName + "(" + lit + ")"
		}
		return lit
	}
	switch {
	case bt.Info()&types.IsInteger != 0:
		c, exact := constant.Int64Val(constant.ToInt(tv.Value))
		if !exact {
			return nil, "the number is too big for PathKit"
		}
		if c == math.MaxInt64 || c == math.MinInt64 {
			return nil, "the number is at the edge of what PathKit can solve"
		}
		v := c
		switch op {
		case token.EQL, token.LEQ, token.GEQ:
			v = c
		case token.NEQ, token.GTR:
			v = c + 1
		case token.LSS:
			v = c - 1
		}
		if bt.Info()&types.IsUnsigned != 0 && v < 0 {
			return nil, "the value would be below zero"
		}
		return []assign{{lhs, conv(strconv.FormatInt(v, 10))}}, ""
	case bt.Info()&types.IsString != 0:
		s := constant.StringVal(tv.Value)
		switch op {
		case token.EQL:
			return []assign{{lhs, conv(strconv.Quote(s))}}, ""
		case token.NEQ:
			return []assign{{lhs, conv(strconv.Quote(s + "x"))}}, ""
		}
		return nil, "PathKit does not solve an ordering of texts"
	case bt.Info()&types.IsBoolean != 0:
		c := constant.BoolVal(tv.Value)
		v := c
		if op == token.NEQ {
			v = !c
		}
		return []assign{{lhs, strconv.FormatBool(v)}}, ""
	}
	return nil, "PathKit does not solve this kind of value"
}

// ---- files ----

// Source joins the tests of one workflow into one Go file.
func Source(tests []Test) ([]byte, error) {
	if len(tests) == 0 {
		return nil, fmt.Errorf("no tests")
	}
	imports := map[string]string{"testing": "testing", "go.temporal.io/sdk/testsuite": "testsuite"}
	seen := map[string]bool{}
	var body bytes.Buffer
	for _, t := range tests {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		for p, n := range t.Imports {
			if prev, ok := imports[p]; ok && prev != n {
				return nil, fmt.Errorf("the tests name the import %s differently (%s and %s)", p, prev, n)
			}
			for q, m := range imports {
				if m == n && q != p {
					return nil, fmt.Errorf("the tests use the import name %s for two packages", n)
				}
			}
			imports[p] = n
		}
		body.WriteString("\n")
		body.WriteString(t.Func)
	}
	var paths []string
	for p := range imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out bytes.Buffer
	out.WriteString("// Code written by pathkit scaffold. Edit it freely: it is yours.\n\n")
	fmt.Fprintf(&out, "package %s\n\nimport (\n", tests[0].Package)
	std := func(p string) bool { return !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") }
	writeImport := func(p string) {
		name := imports[p]
		base := p[strings.LastIndex(p, "/")+1:]
		if name == base {
			fmt.Fprintf(&out, "\t%q\n", p)
		} else {
			fmt.Fprintf(&out, "\t%s %q\n", name, p)
		}
	}
	for _, p := range paths {
		if std(p) {
			writeImport(p)
		}
	}
	out.WriteString("\n")
	for _, p := range paths {
		if !std(p) {
			writeImport(p)
		}
	}
	out.WriteString(")\n")
	out.Write(body.Bytes())
	src, err := format.Source(out.Bytes())
	if err != nil {
		return out.Bytes(), fmt.Errorf("the scaffold is not valid Go (please report it): %v", err)
	}
	return src, nil
}
