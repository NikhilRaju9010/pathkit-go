package model

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// The behaviour fingerprint of a path (docs/DESIGN.md D25): the external steps
// a run of the path makes, in order. Two paths with the same fingerprint do
// the same things as seen from outside the workflow, so they are one scenario.
//
// A step is written as "kind name: outcome", for example "ChargeCard (activity):
// failure", "timer", "loop for i < n: 1+ rounds", "end: completed".
//
// Left out on purpose: logging, metrics, local computation, query handlers and
// plain calls of helpers that make no external step. Anything the model does
// not read (the unread panel) gives the path an "unknown" step that no other
// path shares, so such a path is never merged with another one.

// Kinds of Unread entry that can hide an external step. The others (a result
// never read, an update validator that panics, a result returned directly:
// the call is a step and its outcome is left out of the step) do not hide one.
var unreadHidesSteps = map[string]bool{
	UnreadInterfaceCall: true, UnreadFuncValue: true, UnreadOtherPackage: true,
	UnreadOtherModule: true, UnreadHelper: true, UnreadByName: true,
	UnreadClosure: true,
}

// fingerprinter holds what is worked out once per graph.
type fingerprinter struct {
	g        *Graph
	checked  map[token.Position]bool // calls whose result a decision reads
	failKind map[*Exit]string        // exits that take one kind of failure
	unitCall map[*ast.CallExpr]*HelperUnit
	unread   map[string]map[int]bool // file -> lines of unread entries that hide steps
	unitUnk  map[*HelperUnit]bool    // a unit with such an entry in its code
	assigned map[types.Object]bool   // variables the workflow function changes
	cancels  map[types.Object]bool   // the cancel functions workflow.WithCancel and NewDisconnectedContext return
	nodes    map[ast.Node]nodeResult
}

type nodeResult struct {
	steps   []string
	unknown bool
}

func (g *Graph) fingerprinter() *fingerprinter {
	if g.fp != nil {
		return g.fp
	}
	f := &fingerprinter{g: g, checked: map[token.Position]bool{}, failKind: map[*Exit]string{}, unitCall: map[*ast.CallExpr]*HelperUnit{},
		unread: map[string]map[int]bool{}, unitUnk: map[*HelperUnit]bool{}, assigned: map[types.Object]bool{}, cancels: map[types.Object]bool{}, nodes: map[ast.Node]nodeResult{}}
	for _, j := range g.AllJunctions() {
		if j.call != nil {
			f.checked[posKey(*j.call)] = true
		}
	}
	for _, cs := range g.CallSites() {
		for _, k := range cs.Failures {
			f.failKind[k.Exit] = k.Kind
		}
	}
	for _, u := range g.units {
		for _, c := range u.Calls {
			f.unitCall[c.Call] = u
		}
	}
	for _, u := range g.unread {
		if !unreadHidesSteps[u.Kind] {
			continue
		}
		if f.unread[u.File] == nil {
			f.unread[u.File] = map[int]bool{}
		}
		f.unread[u.File][u.Line] = true
	}
	for _, u := range g.units {
		s, e := g.fset.Position(u.Site.Body.Pos()), g.fset.Position(u.Site.Body.End())
		for l := range f.unread[s.Filename] {
			if l >= s.Line && l <= e.Line {
				f.unitUnk[u] = true
			}
		}
	}
	if g.fn != nil && g.fn.Body != nil && g.info != nil {
		f.collectAssigned(g.fn.Body)
	}
	g.fp = f
	return f
}

func posKey(p token.Position) token.Position {
	return token.Position{Filename: p.Filename, Line: p.Line, Column: p.Column}
}

// collectAssigned notes every variable the workflow function writes after its
// declaration or hands out by address: such a variable may hold different
// values on different paths.
func (f *fingerprinter) collectAssigned(body *ast.BlockStmt) {
	mark := func(e ast.Expr) {
		for {
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				if o := f.g.info.Uses[x]; o != nil {
					f.assigned[o] = true
				}
				return
			case *ast.SelectorExpr:
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			default:
				return
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) == 2 && len(s.Rhs) == 1 {
				if call, ok := ast.Unparen(s.Rhs[0]).(*ast.CallExpr); ok {
					if fn := f.funcOf(call); isPkgFunc(fn, workflowPkg, "WithCancel", "WithCancelCause", "NewDisconnectedContext") {
						if id, ok := s.Lhs[1].(*ast.Ident); ok {
							if o := f.g.info.Defs[id]; o != nil {
								f.cancels[o] = true
							} else if o := f.g.info.Uses[id]; o != nil {
								f.cancels[o] = true
							}
						}
					}
				}
			}
			if s.Tok != token.DEFINE {
				for _, l := range s.Lhs {
					mark(l)
				}
			}
		case *ast.IncDecStmt:
			mark(s.X)
		case *ast.UnaryExpr:
			if s.Op == token.AND {
				mark(s.X)
			}
		case *ast.RangeStmt:
			if s.Tok == token.ASSIGN {
				if s.Key != nil {
					mark(s.Key)
				}
				if s.Value != nil {
					mark(s.Value)
				}
			}
		}
		return true
	})
}

// stable reports whether e has the same value on every path of the workflow:
// a constant, or something read from a workflow parameter that is never changed.
func (f *fingerprinter) stable(e ast.Expr) bool {
	e = ast.Unparen(e)
	if tv, ok := f.g.info.Types[e]; ok && tv.Value != nil {
		return true
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		o := f.g.info.Uses[x]
		if o == nil {
			return false
		}
		if _, isNil := o.(*types.Nil); isNil {
			return true
		}
		v, ok := o.(*types.Var)
		if !ok || f.assigned[o] || v.IsField() {
			return false
		}
		return f.isParam(v)
	case *ast.SelectorExpr:
		return f.stable(x.X)
	case *ast.IndexExpr:
		return f.stable(x.X) && f.stable(x.Index)
	case *ast.StarExpr:
		return f.stable(x.X)
	}
	return false
}

func (f *fingerprinter) isParam(v *types.Var) bool {
	if f.g.fn == nil || f.g.fn.Type == nil {
		return false
	}
	if f.g.fn.Recv != nil {
		for _, fl := range f.g.fn.Recv.List {
			for _, n := range fl.Names {
				if f.g.info.Defs[n] == types.Object(v) {
					return true
				}
			}
		}
	}
	for _, fl := range f.g.fn.Type.Params.List {
		for _, n := range fl.Names {
			if f.g.info.Defs[n] == types.Object(v) {
				return true
			}
		}
	}
	return false
}

// funcOf is the function or method a call invokes (nil for a function value).
func (f *fingerprinter) funcOf(call *ast.CallExpr) *types.Func {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		fn, _ := f.g.info.Uses[fun].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		fn, _ := f.g.info.Uses[fun.Sel].(*types.Func)
		return fn
	}
	return nil
}

// stepsOf are the external steps one statement or expression makes, in the
// order they run, and whether it holds a place PathKit did not read.
func (f *fingerprinter) stepsOf(n ast.Node) nodeResult {
	switch n.(type) {
	case nil, SkipMark, EnterMark:
		return nodeResult{}
	}
	if r, ok := f.nodes[n]; ok {
		return r
	}
	var r nodeResult
	var stack []ast.Node
	ast.Inspect(n, func(x ast.Node) bool {
		if x == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if call, ok := top.(*ast.CallExpr); ok {
				f.callStep(call, &r)
			}
			return true
		}
		if _, lit := x.(*ast.FuncLit); lit {
			stack = append(stack, x)
			return false
		}
		stack = append(stack, x)
		return true
	})
	// an unread entry on a line the node covers
	s, e := f.g.fset.Position(n.Pos()), f.g.fset.Position(n.End())
	for l := range f.unread[s.Filename] {
		if l >= s.Line && l <= e.Line {
			r.unknown = true
		}
	}
	f.nodes[n] = r
	return r
}

func (f *fingerprinter) callStep(call *ast.CallExpr, r *nodeResult) {
	if u, ok := f.unitCall[call]; ok {
		step, unk := f.unitStep(u, call)
		r.steps = append(r.steps, step)
		r.unknown = r.unknown || unk || f.unitUnk[u]
		return
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && f.cancels[f.g.info.Uses[id]] {
		r.steps = append(r.steps, "cancel context")
		return
	}
	fn := f.funcOf(call)
	if fn == nil || fn.Pkg() == nil {
		return
	}
	if idx, isStarter := starterNameArg(fn); isStarter && !f.nameKnown(call, idx) {
		r.unknown = true // the activity or signal is chosen by a value: which one is only known at run time
	}
	if f.checked[posKey(f.g.fset.Position(call.Pos()))] {
		return // the decision on its result writes the step
	}
	name := func(i int) string {
		if i < len(call.Args) {
			return argName(call.Args[i])
		}
		return "?"
	}
	switch {
	case isPkgFunc(fn, workflowPkg, "ExecuteActivity"):
		r.steps = append(r.steps, name(1)+" (activity): unchecked")
	case isPkgFunc(fn, workflowPkg, "ExecuteLocalActivity"):
		r.steps = append(r.steps, name(1)+" (local activity): unchecked")
	case isPkgFunc(fn, workflowPkg, "ExecuteChildWorkflow"):
		r.steps = append(r.steps, name(1)+" (child workflow): unchecked")
	case isNexusExecute(fn):
		r.steps = append(r.steps, name(1)+" (nexus operation): unchecked")
	case isPkgFunc(fn, workflowPkg, "Sleep", "NewTimer", "NewTimerWithOptions"):
		r.steps = append(r.steps, "timer")
	case isPkgFunc(fn, workflowPkg, "SignalExternalWorkflow"):
		r.steps = append(r.steps, "signal sent "+name(3))
	case isPkgFunc(fn, workflowPkg, "RequestCancelExternalWorkflow"):
		r.steps = append(r.steps, "cancel sent")
	case isPkgFunc(fn, workflowPkg, "SideEffect", "MutableSideEffect"):
		r.steps = append(r.steps, "side effect")
	case isPkgFunc(fn, workflowPkg, "UpsertSearchAttributes", "UpsertTypedSearchAttributes"):
		r.steps = append(r.steps, "upsert search attributes")
	case isPkgFunc(fn, workflowPkg, "UpsertMemo"):
		r.steps = append(r.steps, "upsert memo")
	case isPkgFunc(fn, workflowPkg, "NewContinueAsNewError"):
		r.steps = append(r.steps, "continue-as-new")
	case isPkgFunc(fn, workflowPkg, "Await"):
		r.steps = append(r.steps, "wait")
	case isPkgFunc(fn, workflowPkg, "AwaitWithTimeout"):
		r.steps = append(r.steps, "wait with timeout")
	case isSDKMethod(fn, []string{"ReceiveChannel"}, "Receive", "ReceiveWithTimeout", "ReceiveAsync", "ReceiveAsyncWithMoreFlag"):
		recv := "channel"
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			recv = types.ExprString(sel.X)
		}
		r.steps = append(r.steps, "signal received "+recv)
	}
}

// unitStep writes the call of a summarized helper. The helper's own decisions
// are not part of the workflow's path, so two paths that make the same call
// can differ inside it, unless the call has nothing that may differ: then the
// step names the call, and otherwise it is unknown.
func (f *fingerprinter) unitStep(u *HelperUnit, call *ast.CallExpr) (string, bool) {
	args := make([]string, 0, len(call.Args))
	stable := true
	for _, a := range call.Args {
		if t := f.g.info.TypeOf(a); t != nil && strings.HasSuffix(t.String(), "workflow.Context") {
			continue
		}
		args = append(args, types.ExprString(a))
		stable = stable && f.stable(a)
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		stable = stable && f.stable(sel.X)
	}
	if len(u.Junctions) == 0 {
		stable = true // nothing inside it to decide
	}
	step := "helper " + u.Name() + "(" + strings.Join(args, ", ") + ")"
	return step, !stable
}

// stepLabel is what taking one exit adds to a path's fingerprint, apart from
// the nodes the road runs. iterated says the loop of the junction was entered
// earlier on the path (leaving it again is then not a second decision).
func (g *Graph) stepLabel(j *Junction, e *Exit, iterated bool) []string {
	f := g.fingerprinter()
	switch j.Kind {
	case ErrCheck:
		if j.viaHelper && !g.summarized {
			// the check on a followed helper's result: the helper's own decisions,
			// copied into the path, already say what it did
			return nil
		}
		out := j.Label + ": " + e.Label
		if k, ok := f.failKind[e]; ok {
			return []string{out, "failure kind: " + k}
		}
		return []string{out}
	case Selector:
		return []string{"wait for " + e.Label}
	case WaitResult:
		return []string{j.Label + ": " + e.Label}
	case Loop:
		switch e.Label {
		case "iterate":
			return []string{"loop " + j.stable + ": 1+ rounds"}
		case "exit":
			if iterated {
				return nil
			}
			return []string{"loop " + j.stable + ": 0 rounds"}
		}
		return nil
	}
	for _, fact := range j.Facts {
		if fact.Kind == VersionCheck {
			side := "new"
			if fact.Exit == e.Label {
				side = "old"
			}
			return []string{"version " + j.stable + ": " + side}
		}
	}
	if k, ok := f.failKind[e]; ok {
		return []string{"failure kind: " + k}
	}
	return nil
}

// roadSteps are the steps the nodes of a target run.
func (g *Graph) roadSteps(t Target) ([]string, bool) {
	f := g.fingerprinter()
	var out []string
	unk := false
	for _, n := range t.Nodes {
		r := f.stepsOf(n)
		out = append(out, r.steps...)
		unk = unk || r.unknown
	}
	return out, unk
}

func endLabel(k EndKind) string {
	if k == EndUnknown {
		return "end"
	}
	return "end: " + string(k)
}

// Fingerprint is the list of external steps of a path, ending with how it
// ends. A path through a place PathKit did not read has "unknown <path id>" as
// one more step, which no other path has.
func (g *Graph) Fingerprint(p Path) []string {
	out, unk := g.stepsOfRoad(true, p.Steps)
	out = append(out, endLabel(p.End))
	if unk {
		out = append(out, "unknown "+p.ID())
	}
	return out
}

// stepsOfRoad are the external steps of a run of steps; fromStart adds the
// steps the road from the start of the workflow makes.
func (g *Graph) stepsOfRoad(fromStart bool, steps []Step) ([]string, bool) {
	var out []string
	unk := false
	if fromStart {
		out, unk = g.roadSteps(g.Start)
	}
	iterated := map[*Junction]bool{}
	for _, s := range steps {
		out = append(out, g.stepLabel(s.Junction, s.Exit, iterated[s.Junction])...)
		if s.Junction.Kind == Loop && s.Exit.Label == "iterate" {
			iterated[s.Junction] = true
		}
		more, u := g.roadSteps(s.Exit.To)
		out = append(out, more...)
		unk = unk || u
	}
	return out, unk
}

// A Span is a run of steps of a fingerprint: Steps[From:To].
type Span struct{ From, To int }

// FingerprintSpans is Fingerprint with the places where a loop's body is. For
// every loop the path enters ("loop ...: 1+ rounds"), a span says which steps
// belong to the body of its last round, the one the path shows; the rounds
// before it are not on the path. A span runs to the loop's own end (its retry
// step), or to the end of the path when the path leaves the loop another way
// (a return or a break). Spans of loops inside a body lie inside its span.
// The end label is not inside any span.
func (g *Graph) FingerprintSpans(p Path) ([]string, []Span) {
	out, _ := g.roadSteps(g.Start)
	var spans []Span
	var open []int // indexes in spans of the loops being read, innermost last
	iterated := map[*Junction]bool{}
	openOf := map[*Junction]int{}
	for _, s := range p.Steps {
		if s.Junction.Kind == Loop && s.Exit.Label == "retry" {
			if i, ok := openOf[s.Junction]; ok {
				spans[i].To = len(out)
				delete(openOf, s.Junction)
				for k := len(open) - 1; k >= 0; k-- {
					if open[k] == i {
						open = append(open[:k], open[k+1:]...)
						break
					}
				}
			}
		}
		out = append(out, g.stepLabel(s.Junction, s.Exit, iterated[s.Junction])...)
		if s.Junction.Kind == Loop && s.Exit.Label == "iterate" {
			iterated[s.Junction] = true
			spans = append(spans, Span{From: len(out), To: -1})
			openOf[s.Junction] = len(spans) - 1
			open = append(open, len(spans)-1)
		}
		more, _ := g.roadSteps(s.Exit.To)
		out = append(out, more...)
	}
	for _, i := range open {
		spans[i].To = len(out)
	}
	return append(out, endLabel(p.End)), spans
}

// StageFragmentID names what a stage path does outside the workflow, for
// counting how many different things a stage can do. Two stage paths with the
// same ID make the same external steps.
func (g *Graph) StageFragmentID(sp StagePath) string {
	steps, _ := g.stepsOfRoad(sp.Stage == 0, sp.Steps)
	if !sp.Continue {
		steps = append(steps, endLabel(sp.End))
	}
	return ScenarioID(steps)
}

// ScenarioID names a scenario by its steps: the same steps give the same ID
// whatever else changes in the code.
func ScenarioID(steps []string) string {
	sum := sha256.Sum256([]byte(strings.Join(steps, "\x1f")))
	return "sc:" + hex.EncodeToString(sum[:])[:10]
}

// EventLoop reports a workflow that waits for events in a loop that has no
// condition: a "for" whose body holds a Selector's Select call or a receive on
// a signal channel. Such a workflow has no useful path count (each trip of the
// loop handles one event), so its main number is handler coverage.
func (g *Graph) EventLoop() bool {
	f := g.fingerprinter()
	for _, j := range g.Junctions {
		if j.Kind != Loop {
			continue
		}
		fs, ok := j.Stmt.(*ast.ForStmt)
		if !ok || fs.Cond != nil {
			continue
		}
		found := false
		ast.Inspect(fs.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || found {
				return !found
			}
			fn := f.funcOf(call)
			if isSDKMethod(fn, []string{"Selector"}, "Select") ||
				isSDKMethod(fn, []string{"ReceiveChannel"}, "Receive", "ReceiveWithTimeout", "ReceiveAsync", "ReceiveAsyncWithMoreFlag") {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// starterNameArg says which argument names what a call starts or sends.
func starterNameArg(fn *types.Func) (int, bool) {
	switch {
	case isPkgFunc(fn, workflowPkg, "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow"), isNexusExecute(fn):
		return 1, true
	case isPkgFunc(fn, workflowPkg, "SignalExternalWorkflow"):
		return 3, true
	}
	return 0, false
}

// nameKnown reports whether the argument names one fixed function or text: a
// function, or a constant string. A variable (which may hold different
// functions on different paths) is not.
func (f *fingerprinter) nameKnown(call *ast.CallExpr, idx int) bool {
	if idx >= len(call.Args) {
		return true
	}
	a := ast.Unparen(call.Args[idx])
	if tv, ok := f.g.info.Types[a]; ok && tv.Value != nil {
		return true
	}
	switch x := a.(type) {
	case *ast.Ident:
		_, isFn := f.g.info.Uses[x].(*types.Func)
		return isFn
	case *ast.SelectorExpr:
		_, isFn := f.g.info.Uses[x.Sel].(*types.Func)
		return isFn
	}
	return false
}
