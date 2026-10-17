package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/cfg"
)

// A helper is a function of the workflow's own package that a workflow calls
// and that does workflow work itself: it calls the Temporal SDK. Its error
// result matters: what the workflow does with it (usually "if err != nil")
// is a real decision, not one to assume away (docs/DESIGN.md D2, D15).
//
// Three kinds of callee are known statically: a function, a method of a
// concrete type, and a local variable that is set exactly once to a function
// literal and called by name. A call through an interface, or through a
// function value PathKit cannot trace to one literal, stays opaque.
type helperFn struct {
	name string // as written at the call: "charge", "o.charge"
	decl *ast.FuncDecl
	lit  *ast.FuncLit
	typ  *ast.FuncType
	body *ast.BlockStmt
	file *ast.File
}

// helperDecls indexes every function declaration of the workflow's package.
func (b *builder) helperDecls() map[*types.Func]*helperFn {
	if b.decls != nil {
		return b.decls
	}
	b.decls = map[*types.Func]*helperFn{}
	for _, f := range b.wf.Pkg.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, ok := b.info.Defs[fd.Name].(*types.Func); ok {
				b.decls[fn] = &helperFn{name: fd.Name.Name, decl: fd, typ: fd.Type, body: fd.Body, file: f}
			}
		}
	}
	return b.decls
}

// calleeOf finds the function a call invokes, when its body is known.
func (b *builder) calleeOf(call *ast.CallExpr) *helperFn {
	if f := b.calledFunc(call); f != nil {
		if h := b.helperDecls()[f]; h != nil {
			cp := *h
			cp.name = nameAt(call)
			return &cp
		}
		return nil
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if rhs, ok := b.singleDefinition(id); ok {
			if lit, ok := ast.Unparen(rhs).(*ast.FuncLit); ok {
				return &helperFn{name: id.Name, lit: lit, typ: lit.Type, body: lit.Body, file: b.wf.File}
			}
		}
	}
	return nil
}

// nameAt writes the callee as it is written at the call.
func nameAt(call *ast.CallExpr) string {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return types.ExprString(f)
	}
	return types.ExprString(call.Fun)
}

// isHelper reports whether the callee does workflow work: it calls the
// Temporal SDK somewhere in its own code (function literals inside it
// count: they run when it runs, or later).
func (b *builder) isHelper(h *helperFn) bool {
	return b.containsTemporalCall(h.body)
}

// errResult returns the index of the error result of a function, or -1.
func (b *builder) errResult(typ *ast.FuncType) int {
	if typ.Results == nil {
		return -1
	}
	n := 0
	last := -1
	for _, f := range typ.Results.List {
		c := len(f.Names)
		if c == 0 {
			c = 1
		}
		for i := 0; i < c; i++ {
			if t := b.info.TypeOf(f.Type); t != nil && types.Identical(t, errorType) {
				last = n
			}
			n++
		}
	}
	if last == n-1 {
		return last
	}
	return -1 // only an error as the LAST result is followed
}

// retKind says what a return statement of a helper returns as its error.
type retKind int

const (
	retUnknown retKind = iota // may be nil or not: not followed
	retNil                    // the error result is nil
	retError                  // the error result is certainly not nil
)

// returnKind classifies the error result of one return statement of h.
func (b *builder) returnKind(h *helperFn, ret *ast.ReturnStmt, errIdx int) (retKind, ast.Expr) {
	if errIdx < 0 {
		return retNil, nil
	}
	if namedResults(h.typ) && hasDefer(h.body) {
		return retUnknown, nil // a deferred function may change a named result after the return
	}
	if len(ret.Results) == 0 {
		return retUnknown, nil // a bare return with named results
	}
	if len(ret.Results) == 1 && errIdx > 0 {
		return retUnknown, nil // return f(): several results from one call
	}
	if errIdx >= len(ret.Results) {
		return retUnknown, nil
	}
	e := ast.Unparen(ret.Results[errIdx])
	if b.isNil(e) {
		return retNil, e
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		f := b.calledFunc(x)
		if isPkgFunc(f, "fmt", "Errorf") || isPkgFunc(f, "errors", "New") ||
			(isPkgFunc(f, temporalPkg) && strings.HasPrefix(f.Name(), "New") && strings.HasSuffix(f.Name(), "Error")) {
			return retError, e
		}
	case *ast.Ident:
		if b.knownNonNilIn(h, b.info.Uses[x], ret) && b.assignedOnce(h, b.info.Uses[x]) {
			return retError, e
		}
	}
	return retUnknown, e
}

// knownNonNilIn: ret sits on the "error" side of an if that checks obj
// against nil, inside h.
func (b *builder) knownNonNilIn(h *helperFn, obj types.Object, ret *ast.ReturnStmt) bool {
	return b.knownNonNilFile(h.file, obj, ret)
}

// returnsOf lists the return statements in h's own code (not in function
// literals inside it).
func returnsOf(body *ast.BlockStmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, n)
		}
		return true
	})
	return out
}

// errorSummary says how a helper can fail.
type errorSummary struct {
	// never: it returns no error, or only nil.
	never bool
	// calls are the modelled Temporal calls whose errors it returns, when
	// every error it returns is one of those ("it fails exactly when they
	// fail"); empty with never false means the errors are not followed: the
	// error is unknown, not "none".
	calls []string
}

// summarize works out how h can fail. depth bounds the helpers a helper
// calls (a recursion is treated as unknown).
func (b *builder) summarize(h *helperFn, depth int, seen map[ast.Node]bool) errorSummary {
	idx := b.errResult(h.typ)
	if idx < 0 {
		return errorSummary{never: true}
	}
	if depth > 3 || seen[h.body] {
		return errorSummary{}
	}
	seen[h.body] = true
	defer delete(seen, h.body)
	var calls []string
	failing := false
	for _, ret := range returnsOf(h.body) {
		kind, e := b.returnKind(h, ret, idx)
		switch kind {
		case retNil:
			continue
		case retError:
			// fmt.Errorf, errors.New, ...: an error of the helper's own making,
			// not the error of a modelled call. (A variable that an error check
			// just found non-nil is followed back to its call below.)
			if _, isVar := e.(*ast.Ident); !isVar {
				return errorSummary{}
			}
		}
		failing = true
		label, ok := b.modelledError(h, e, ret, depth, seen)
		if !ok {
			return errorSummary{}
		}
		for _, l := range label {
			if !contains(calls, l) {
				calls = append(calls, l)
			}
		}
	}
	if !failing {
		return errorSummary{never: true}
	}
	return errorSummary{calls: calls}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// modelledError: the error expression e returned by ret is the error of
// modelled Temporal calls; their labels.
func (b *builder) modelledError(h *helperFn, e ast.Expr, ret *ast.ReturnStmt, depth int, seen map[ast.Node]bool) ([]string, bool) {
	if e == nil {
		return nil, false
	}
	e = ast.Unparen(e)
	// return fut.Get(ctx, nil) / return workflow.Sleep(...) / return helper2(...)
	if call, ok := e.(*ast.CallExpr); ok {
		if l, ok := b.callErrorLabels(call, depth, seen); ok {
			return l, true
		}
		return nil, false
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil, false
	}
	obj := b.info.Uses[id]
	if obj == nil {
		return nil, false
	}
	rhs, _, found := b.nearestAssign(obj, ret.Pos())
	if !found {
		return nil, false
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	return b.callErrorLabels(call, depth, seen)
}

// callErrorLabels: the error of this call is the error of modelled Temporal
// calls (the call itself, or a helper's summary).
func (b *builder) callErrorLabels(call *ast.CallExpr, depth int, seen map[ast.Node]bool) ([]string, bool) {
	f := b.calledFunc(call)
	switch {
	case isSDKMethod(f, []string{"Future"}, "Get"):
		sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		return []string{b.futureLabel(sel.X, call, 0)}, true
	case isPkgFunc(f, workflowPkg, "Sleep"):
		return []string{"Sleep (timer)"}, true
	case isPkgFunc(f, workflowPkg, "Await", "AwaitWithTimeout"):
		return []string{f.Name() + " (wait)"}, true
	}
	if h := b.calleeOf(call); h != nil && b.isHelper(h) {
		s := b.summarize(h, depth+1, seen)
		if len(s.calls) > 0 {
			return s.calls, true
		}
	}
	return nil, false
}

// helperErrLabel is the label of an error check on the result of a call to a
// helper, and whether that check is a decision (a junction). A helper that
// never fails is not (the check is assumed to pass, as for any other
// non-Temporal call); one that fails exactly when modelled calls fail is,
// labelled with them; one whose errors are not followed is, as an unknown
// error.
func (b *builder) helperErrLabel(call *ast.CallExpr) (string, bool) {
	h := b.calleeOf(call)
	if h == nil || !(b.isHelper(h) || (!b.noFollow && b.worthInlining(h, 0))) {
		return "", false
	}
	s := b.summarize(h, 0, map[ast.Node]bool{})
	switch {
	case s.never:
		return "", false
	case len(s.calls) > 0:
		return strings.Join(s.calls, " or ") + ", via " + h.name, true
	}
	return h.name + " (helper)", true
}

// maxHelperDepth is how many helpers deep a call is followed: a call in the
// workflow, in a helper, in a helper of that, and in a helper of that.
const maxHelperDepth = 3

// inlineCall is a call whose helper's code becomes part of the graph.
type inlineCall struct {
	call *ast.CallExpr
	h    *helperFn
	ctx  ast.Expr
	// errObj is the error variable the call's error result is stored in,
	// when it is stored in one (err := h(ctx)).
	errObj types.Object
	// ret: the call is the whole of a return statement, so the helper's
	// return is the caller's return.
	ret bool
}

// determined says the helper that just returned also decided the caller's
// error check on obj: it returned nil (fails false) or certainly an error.
// The caller's "if err != nil" is still a step of the path (it is recorded
// when it runs), but it has only one possible exit (Target.Only).
type determined struct {
	obj   types.Object
	fails bool
}

// inlineAt reports whether the node is a statement that calls a helper
// whose code is followed into. The call has to be the whole statement (or
// the whole right-hand side of an assignment, or the whole of a return), so
// that nothing else in it runs between the call and the helper's code.
func (b *builder) inlineAt(n ast.Node) *inlineCall {
	var call *ast.CallExpr
	ic := &inlineCall{}
	var lhs ast.Expr
	switch s := n.(type) {
	case *ast.ExprStmt:
		call, _ = ast.Unparen(s.X).(*ast.CallExpr)
	case *ast.AssignStmt:
		if (s.Tok == token.ASSIGN || s.Tok == token.DEFINE) && len(s.Rhs) == 1 {
			call, _ = ast.Unparen(s.Rhs[0]).(*ast.CallExpr)
			lhs = s.Lhs[len(s.Lhs)-1]
		}
	case *ast.ReturnStmt:
		if len(s.Results) == 1 {
			call, _ = ast.Unparen(s.Results[0]).(*ast.CallExpr)
			ic.ret = true
		}
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR && len(gd.Specs) == 1 {
			if vs, ok := gd.Specs[0].(*ast.ValueSpec); ok && len(vs.Values) == 1 {
				call, _ = ast.Unparen(vs.Values[0]).(*ast.CallExpr)
				lhs = vs.Names[len(vs.Names)-1]
			}
		}
	}
	if call == nil {
		return nil
	}
	h, ctx := b.inlinable(call)
	if h == nil {
		return nil
	}
	ic.call, ic.h, ic.ctx = call, h, ctx
	if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" && b.errResult(h.typ) >= 0 {
		if v, ok := b.info.ObjectOf(id).(*types.Var); ok && types.Identical(v.Type(), errorType) {
			ic.errObj = v
		}
	}
	return ic
}

// inlinable decides whether the code of the function a call invokes is
// followed into from the code being walked, and returns the function with
// the call's argument that carries the workflow context.
func (b *builder) inlinable(call *ast.CallExpr) (*helperFn, ast.Expr) {
	if b.noFollow {
		return nil, nil
	}
	h := b.calleeOf(call)
	if h == nil || call.Ellipsis != token.NoPos {
		return nil, nil
	}
	if b.cur.Depth() >= maxHelperDepth || h.body == b.wf.Func.Body {
		return nil, nil
	}
	for s := b.cur; s != nil; s = s.Parent {
		if s.Body == h.body {
			return nil, nil // a helper that calls itself, or a helper it was reached from
		}
	}
	if b.isWorkflowFunc(h) {
		return nil, nil // another workflow has a map of its own; calling it is not a helper
	}
	idx, _ := b.ctxParam(h.typ)
	if idx < 0 || idx >= len(call.Args) {
		return nil, nil
	}
	if !b.helperClean(h) || !b.worthInlining(h, b.cur.Depth()) {
		return nil, nil
	}
	return h, call.Args[idx]
}

// ctxParam is the index of the first parameter of type workflow.Context that
// has a name (the recorder hands its identity over through it), and the
// name; -1 when there is none.
func (b *builder) ctxParam(typ *ast.FuncType) (int, *ast.Ident) {
	n := 0
	for _, f := range typ.Params.List {
		count := len(f.Names)
		if count == 0 {
			count = 1
		}
		if t := b.info.TypeOf(f.Type); t != nil && isWorkflowContext(t) {
			if len(f.Names) == 0 {
				return -1, nil
			}
			return n, f.Names[0]
		}
		if _, variadic := f.Type.(*ast.Ellipsis); variadic {
			return -1, nil
		}
		n += count
	}
	return -1, nil
}

func isWorkflowContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Name() != "Context" {
		return false
	}
	p := named.Obj().Pkg().Path()
	return p == workflowPkg || p == internalPkg
}

// helperClean: a helper's code holds nothing PathKit cannot map (goto,
// Go's select statement, workflow.Selector, a type parameter).
func (b *builder) helperClean(h *helperFn) bool {
	if h.decl != nil && (h.decl.Type.TypeParams != nil || (h.decl.Recv != nil && b.genericRecv(h.decl))) {
		return false
	}
	clean := true
	ast.Inspect(h.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				clean = false
			}
		case *ast.SelectStmt:
			clean = false
		case *ast.CallExpr:
			if b.isSelectorMethod(n, "Select") {
				clean = false
			}
		}
		return clean
	})
	return clean
}

func (b *builder) genericRecv(fd *ast.FuncDecl) bool {
	t := b.info.TypeOf(fd.Recv.List[0].Type)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.TypeParams().Len() > 0
}

// worthInlining: the helper does workflow work: it calls the Temporal SDK
// itself or calls a helper that does. (A helper that never touches the
// workflow API, however many ifs it has, is ordinary code: what it decides
// is not a step of the workflow, as before.)
func (b *builder) worthInlining(h *helperFn, depth int) bool {
	if w, ok := b.worth[h.body]; ok {
		return w
	}
	if b.worthBusy[h.body] || depth > maxHelperDepth {
		b.worthCycles++
		return false // a cycle of helpers: the answer comes from the rest
	}
	b.worthBusy[h.body] = true
	cyclesBefore := b.worthCycles
	w := b.computeWorth(h, depth)
	delete(b.worthBusy, h.body)
	if w || b.worthCycles == cyclesBefore {
		b.worth[h.body] = w // a "no" that leaned on a cycle depends on where the walk started: not remembered
	}
	return w
}

func (b *builder) computeWorth(h *helperFn, depth int) bool {
	if b.containsTemporalCall(h.body) {
		return true
	}
	worth := false
	ast.Inspect(h.body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if c := b.calleeOf(call); c != nil && c.body != h.body && b.worthInlining(c, depth+1) {
				worth = true
			}
		}
		return !worth
	})
	return worth
}

// siteFor makes (or finds) the site for a call in the code being walked.
func (b *builder) siteFor(ic *inlineCall) *Site {
	k := siteKey{b.cur, ic.call}
	if s := b.sites[k]; s != nil {
		return s
	}
	h := ic.h
	_, ctxName := b.ctxParam(h.typ)
	s := &Site{Parent: b.cur, Helper: h.name, Call: ic.call, Ctx: ic.ctx, CtxName: ctxName, Type: h.typ, Body: h.body, File: h.file, Decl: h.decl,
		Pos: b.fset.Position(ic.call.Pos()), order: b.orderAt(ic.call.Pos()), h: h}
	p := b.fset.Position(h.body.Pos())
	s.Key = fmt.Sprintf("%s:%d:%d", filepath.Base(p.Filename), p.Line, p.Column)
	b.sites[k] = s
	b.siteList = append(b.siteList, s)
	return s
}

// intoHelper is the road into a helper's code, from the call at node i of
// blk. A return in the helper, or its end, goes on to what follows the call
// (node i+1 of blk); the kind of error the return gives decides the
// caller's error check when it can (see determined).
func (b *builder) intoHelper(ic *inlineCall, blk *cfg.Block, i int) Target {
	parent := b.contOf[blk]
	s := b.siteFor(ic)
	c := cfg.New(ic.h.body, b.mayReturn)
	after := func(k retKind) Target {
		if ic.ret {
			if parent != nil {
				return parent(k)
			}
			return Target{End: b.endFromKind(k)}
		}
		var d determined
		if k == retNil && b.assumesInside(ic.h) {
			k = retUnknown // the walk took "no error" at a check it only assumes; the run may not
		}
		if ic.errObj != nil && (k == retNil || k == retError) && b.detSafe(blk, i+1, ic.errObj) {
			d = determined{obj: ic.errObj, fails: k == retError}
		}
		return b.roadFromDet(blk, i+1, d)
	}
	for _, cb := range c.Blocks {
		b.contOf[cb] = after
		b.siteOf[cb] = s
	}
	prev := b.cur
	b.cur = s
	b.indexSwitchesIn(ic.h.body)
	b.indexLoops(c)
	t := b.road(c.Blocks[0])
	b.cur = prev
	t.Nodes = append([]ast.Node{EnterMark{Site: s}}, t.Nodes...)
	return t
}

// endFromKind is how the workflow ends when it returns a helper's result.
func (b *builder) endFromKind(k retKind) EndKind {
	if b.errResult(b.wf.Func.Type) < 0 {
		return EndUnknown
	}
	switch k {
	case retNil:
		return EndCompleted
	case retError:
		return EndFailed
	}
	return EndUnknown
}

// returnedKind is what a return of the code in blk (a helper's, when blk is
// in one) gives as the error result; ret is nil at the end of the code.
func (b *builder) returnedKind(blk *cfg.Block, ret *ast.ReturnStmt) retKind {
	s := b.siteOf[blk]
	if s == nil {
		return retUnknown
	}
	idx := b.errResult(s.h.typ)
	if idx < 0 {
		return retNil
	}
	if ret == nil {
		return retUnknown
	}
	k, _ := b.returnKind(s.h, ret, idx)
	return k
}

// detSafe: from node `from` of blk the road runs straight to an error check
// on obj (the last thing in the block), meets no other helper or Select,
// and never assigns obj; so the value the helper returned is the one checked.
func (b *builder) detSafe(blk *cfg.Block, from int, obj types.Object) bool {
	if len(blk.Succs) != 2 {
		return false
	}
	ifs, ok := blk.Succs[0].Stmt.(*ast.IfStmt)
	if !ok || blk.Succs[0].Kind != cfg.KindIfThen {
		return false
	}
	if o, _, isErr := b.errForm(ifs.Cond); !isErr || o != obj {
		return false
	}
	safe := true
	for _, n := range blk.Nodes[from:] {
		if b.inlineAt(n) != nil {
			return false
		}
		if s, ok := n.(ast.Stmt); ok && b.selectAt[s] != nil {
			return false
		}
		ast.Inspect(n, func(m ast.Node) bool {
			switch m := m.(type) {
			case *ast.AssignStmt:
				for _, l := range m.Lhs {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok && (b.info.Uses[id] == obj || b.info.Defs[id] == obj) {
						safe = false
					}
				}
			case *ast.UnaryExpr:
				if m.Op == token.AND {
					safe = false
				}
			case *ast.FuncLit:
				safe = false // it may run now and write obj
			}
			return safe
		})
	}
	return safe
}

// namedResults: the function gives its results names.
func namedResults(typ *ast.FuncType) bool {
	if typ.Results == nil {
		return false
	}
	for _, f := range typ.Results.List {
		if len(f.Names) > 0 {
			return true
		}
	}
	return false
}

// hasDefer: the function's own code (not a function literal inside it) has a
// defer statement.
func hasDefer(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			found = true
		}
		return !found
	})
	return found
}

// isWorkflowFunc: the function is one PathKit finds as a workflow (exported,
// first parameter workflow.Context, last result error; see internal/discover).
func (b *builder) isWorkflowFunc(h *helperFn) bool {
	if h.decl == nil || !h.decl.Name.IsExported() {
		return false
	}
	if i, _ := b.ctxParam(h.typ); i != 0 {
		return false
	}
	return b.errResult(h.typ) >= 0 && b.errResult(h.typ) == countResults(h.typ)-1
}

func countResults(typ *ast.FuncType) int {
	n := 0
	if typ.Results != nil {
		for _, f := range typ.Results.List {
			n += max(1, len(f.Names))
		}
	}
	return n
}

// assumesInside: the helper has an if or a loop that the walk passes without
// deciding it (an error check on a call that is not Temporal's, an if marked
// //pathkit:ignore, a loop with nothing to decide). Which side a run takes
// there is not on the map, so what the helper returns after it is not certain.
func (b *builder) assumesInside(h *helperFn) bool {
	assumed := false
	ast.Inspect(h.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			if !b.ifIsJunction(n) {
				assumed = true
			}
		case *ast.ForStmt, *ast.RangeStmt:
			if t, in := b.loopReasons(n.(ast.Stmt)); !t && !in && !b.loopHasHelper(n.(ast.Stmt)) {
				assumed = true
			}
		}
		return !assumed
	})
	return assumed
}

// assignedOnce: obj is given a value in exactly one place in the helper and
// its address is never taken, so a return of it later returns that value.
func (b *builder) assignedOnce(h *helperFn, obj types.Object) bool {
	n := 0
	ast.Inspect(h.body, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if id, ok := ast.Unparen(l).(*ast.Ident); ok && (b.info.Uses[id] == obj || b.info.Defs[id] == obj) {
					n++
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				if b.info.Defs[id] == obj {
					n += 2 // declared with a value or the zero value: not a call's result
				}
			}
		case *ast.UnaryExpr:
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok && x.Op == token.AND && b.info.Uses[id] == obj {
				n += 2
			}
		}
		return true
	})
	return n == 1
}
