package facts

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Summaries of plain functions (milestone M11, E2).
//
// A function of the same Go module that does not take a workflow.Context is
// read once, and what it returns is written down in a summary: the dynamic
// types of an interface result, whether a result is nil, a constant result,
// and, for a map the function builds itself, the types it stores under each
// constant string key. The engine uses the summary at a call
// (`m := item.ToMap()`), so a type switch on `m["Items"]` can be settled.
//
// A summary is only written when the function is simple enough to be sure:
// no named results, no defer, go or recover, no closure that touches the map,
// no write to the map under a key that is not a constant, no delete, and the
// map is never handed to other code before it is returned. Depth is limited
// to 3 calls, a function that calls itself is not summarized, interface calls
// and function values are unknown. Unknown means "nothing is known": it can
// only make a path look possible, never impossible.

// maxSummaryDepth is how many nested calls a summary may look through.
const maxSummaryDepth = 3

// fnSummary is what is known about the results of one function.
type fnSummary struct {
	fn      *types.Func
	file    string // base name of the file the function is in
	line    int
	results []*resSummary // one per result; nil: nothing known
}

// resSummary is what is known about one result.
type resSummary struct {
	// types and mayNil: the dynamic types of an interface result (typesKnown).
	typesKnown bool
	types      []types.Type
	mayNil     bool
	// alwaysNil and neverNil: the result is nil on every return, or on none.
	alwaysNil, neverNil bool
	// cnst: every return gives this constant.
	cnst constant.Value
	// mapKnown: the result is a map the function builds; perKey says what each
	// constant key may hold, order is the keys in sorted order.
	mapKnown bool
	perKey   map[string]*keyEntry
	order    []string
}

// keyEntry is what a map may hold under one constant key. known is false when
// some value stored there has a type the summary cannot tell.
type keyEntry struct {
	known  bool
	types  []types.Type
	mayNil bool
}

func (e *keyEntry) addTypes(ts []types.Type, mayNil, known bool) {
	if !known {
		e.known = false
		return
	}
	for _, t := range ts {
		found := false
		for _, o := range e.types {
			if types.Identical(o, t) {
				found = true
			}
		}
		if !found {
			e.types = append(e.types, t)
		}
	}
	e.mayNil = e.mayNil || mayNil
}

// callee finds the function a call runs, when it is decided by the code alone:
// a plain function, a function of another package, or a method on a concrete
// type. A call through an interface or a function value has no callee.
func (a *Analyzer) callee(call *ast.CallExpr) *types.Func { return calleeIn(a.info, call) }

// calleeIn is callee for code with its own types.Info (another package).
func calleeIn(info *types.Info, call *ast.CallExpr) *types.Func {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		fn, _ := info.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if sel := info.Selections[f]; sel != nil {
			if sel.Kind() != types.MethodVal || isInterface(sel.Recv()) {
				return nil
			}
			if p, ok := types.Unalias(sel.Recv()).(*types.Pointer); ok && isInterface(p.Elem()) {
				return nil
			}
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		fn, _ := info.Uses[f.Sel].(*types.Func) // pkg.Func
		return fn
	}
	return nil
}

// summaryOf returns the summary of the function a call runs, or nil.
func (a *Analyzer) summaryOf(call *ast.CallExpr, depth int) *fnSummary {
	fn := a.callee(call)
	if fn == nil {
		return nil
	}
	return a.summarize(fn, depth)
}

func (a *Analyzer) summarize(fn *types.Func, depth int) *fnSummary {
	if depth >= maxSummaryDepth || fn.Pkg() == nil {
		return nil
	}
	if a.sums == nil {
		a.sums = map[*types.Func]*fnSummary{}
	}
	if s, seen := a.sums[fn]; seen {
		return s // nil while it is being worked out: a call that comes back to itself is unknown
	}
	a.sums[fn] = nil
	sum := a.buildSummary(fn, depth)
	a.sums[fn] = sum
	return sum
}

// calleeCode finds the declaration of fn in a package of the module whose
// source was read.
func (a *Analyzer) calleeCode(fn *types.Func) (*ast.FuncDecl, *packages.Package) {
	pkg := a.g.ModulePackage(fn.Pkg().Path())
	if pkg == nil {
		return nil, nil
	}
	want := a.fset.Position(fn.Pos())
	for _, f := range pkg.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name.Name != fn.Name() {
				continue
			}
			got := a.fset.Position(fd.Name.Pos())
			if got.Filename == want.Filename && got.Offset == want.Offset {
				if buildDependent(f, got.Filename) {
					return nil, nil // another build (other system or tags) may use other code
				}
				return fd, pkg
			}
		}
	}
	return nil, nil
}

// calleeCtx is the code being read: one function of one package.
type calleeCtx struct {
	a     *Analyzer
	pkg   *packages.Package
	info  *types.Info
	decl  *ast.FuncDecl
	depth int
}

func (a *Analyzer) buildSummary(fn *types.Func, depth int) *fnSummary {
	decl, pkg := a.calleeCode(fn)
	if decl == nil {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 || decl.Type.TypeParams != nil || sig.TypeParams().Len() > 0 {
		return nil
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if isWorkflowContext(sig.Params().At(i).Type()) {
			return nil // a function that takes the workflow context is followed as a helper, not summarized
		}
	}
	if r := sig.Recv(); r != nil {
		if named := namedOf(r.Type()); named != nil && named.TypeParams().Len() > 0 {
			return nil
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		if sig.Results().At(i).Name() != "" {
			return nil // named results can be set anywhere, and by a defer
		}
	}
	cc := &calleeCtx{a: a, pkg: pkg, info: pkg.TypesInfo, decl: decl, depth: depth}
	if !cc.simple() {
		return nil
	}
	rets := cc.returns()
	if len(rets) == 0 {
		return nil
	}
	pos := a.fset.Position(decl.Name.Pos())
	sum := &fnSummary{fn: fn, file: filepath.Base(pos.Filename), line: pos.Line}
	n := sig.Results().Len()
	any := false
	for i := 0; i < n; i++ {
		var exprs []ast.Expr
		complete := true
		for _, r := range rets {
			switch {
			case len(r.Results) == n:
				exprs = append(exprs, r.Results[i])
			default: // "return f()" with several results, or a bare return
				complete = false
			}
		}
		if !complete {
			sum.results = append(sum.results, cc.forwarded(rets, i, n))
		} else {
			sum.results = append(sum.results, cc.resultSummary(exprs, sig.Results().At(i).Type()))
		}
		if sum.results[i] != nil {
			any = true
		}
	}
	if !any {
		return nil
	}
	return sum
}

func isWorkflowContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "go.temporal.io/sdk/workflow" && named.Obj().Name() == "Context"
}

func namedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}

// simple: no defer, no go statement, no recover, no goto: nothing runs after a
// return or beside it, so what the return statements give is what comes back.
func (cc *calleeCtx) simple() bool {
	ok := true
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.DeferStmt, *ast.GoStmt:
			ok = false
		case *ast.BranchStmt:
			if x.Tok == token.GOTO {
				ok = false
			}
		case *ast.CallExpr:
			if id, isID := ast.Unparen(x.Fun).(*ast.Ident); isID {
				if b, isB := cc.info.Uses[id].(*types.Builtin); isB && b.Name() == "recover" {
					ok = false
				}
			}
		}
		return ok
	})
	return ok
}

// returns lists the return statements of the function itself (not of the
// function literals inside it).
func (cc *calleeCtx) returns() []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, x)
		}
		return true
	})
	return out
}

// forwarded: every return is `return g(...)` with the same number of results
// (or a bare return, which is unknown). The result is g's.
func (cc *calleeCtx) forwarded(rets []*ast.ReturnStmt, i, n int) *resSummary {
	var merged *resSummary
	for _, r := range rets {
		if len(r.Results) != 1 {
			return nil
		}
		call, ok := ast.Unparen(r.Results[0]).(*ast.CallExpr)
		if !ok {
			return nil
		}
		fn := cc.calleeOf(call)
		if fn == nil {
			return nil
		}
		sum := cc.a.summarize(fn, cc.depth+1)
		if sum == nil || i >= len(sum.results) || sum.results[i] == nil {
			return nil
		}
		merged = mergeResults(merged, sum.results[i])
		if merged == nil {
			return nil
		}
	}
	return merged
}

// calleeOf is Analyzer.callee in the code being read (its own types.Info).
func (cc *calleeCtx) calleeOf(call *ast.CallExpr) *types.Func { return calleeIn(cc.info, call) }

func mergeResults(a, b *resSummary) *resSummary {
	if a == nil {
		c := *b
		return &c
	}
	if b == nil {
		return nil
	}
	out := &resSummary{}
	if a.typesKnown && b.typesKnown {
		out.typesKnown = true
		e := &keyEntry{known: true}
		e.addTypes(a.types, a.mayNil, true)
		e.addTypes(b.types, b.mayNil, true)
		out.types, out.mayNil = e.types, e.mayNil
	}
	out.alwaysNil = a.alwaysNil && b.alwaysNil
	out.neverNil = a.neverNil && b.neverNil
	if a.cnst != nil && b.cnst != nil && constant.Compare(a.cnst, token.EQL, b.cnst) {
		out.cnst = a.cnst
	}
	if !out.typesKnown && !out.alwaysNil && !out.neverNil && out.cnst == nil {
		return nil
	}
	return out
}

// resultSummary works out one result from the expressions the returns give.
func (cc *calleeCtx) resultSummary(exprs []ast.Expr, rt types.Type) *resSummary {
	out := &resSummary{}
	known := false
	if isInterface(rt) {
		e := &keyEntry{known: true}
		for _, x := range exprs {
			ts, mayNil, ok := cc.typesOf(x, map[*types.Var]bool{})
			if !ok {
				e.known = false
				break
			}
			e.addTypes(ts, mayNil, true)
		}
		if e.known {
			out.typesKnown, out.types, out.mayNil = true, e.types, e.mayNil
			out.neverNil = !e.mayNil && len(e.types) > 0
			out.alwaysNil = len(e.types) == 0 && e.mayNil
			known = true
		}
	}
	if m, ok := types.Unalias(rt).Underlying().(*types.Map); ok && isEmptyInterface(m.Elem()) {
		if b, isBasic := m.Key().Underlying().(*types.Basic); isBasic && b.Kind() == types.String {
			if per, order, ok := cc.mapResult(exprs); ok {
				out.mapKnown, out.perKey, out.order = true, per, order
				known = true
			}
		}
	}
	if !isInterface(rt) {
		switch cc.nilness(exprs) {
		case 1:
			out.alwaysNil = true
			known = true
		case 2:
			out.neverNil = true
			known = true
		}
	}
	if c := cc.constOf(exprs); c != nil {
		out.cnst = c
		known = true
	}
	if !known {
		return nil
	}
	return out
}

// nilness: 1 when every expression is nil, 2 when none can be, 0 otherwise.
func (cc *calleeCtx) nilness(exprs []ast.Expr) int {
	allNil, allNon := true, true
	for _, e := range exprs {
		e = ast.Unparen(e)
		if tv, ok := cc.info.Types[e]; ok && tv.IsNil() {
			allNon = false
			continue
		}
		allNil = false
		switch x := e.(type) {
		case *ast.UnaryExpr:
			if _, lit := ast.Unparen(x.X).(*ast.CompositeLit); !(x.Op == token.AND && lit) {
				allNon = false
			}
		case *ast.CompositeLit:
		case *ast.CallExpr:
			isMake := false
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				if b, is := cc.info.Uses[id].(*types.Builtin); is && (b.Name() == "make" || b.Name() == "new") {
					isMake = true
				}
			}
			if !isMake {
				allNon = false
			}
		default:
			allNon = false
		}
	}
	switch {
	case len(exprs) > 0 && allNil:
		return 1
	case len(exprs) > 0 && allNon:
		return 2
	}
	return 0
}

// constOf: every return gives the same constant, written in a way that is the
// same on every machine (a literal, true, false or a constant of the module).
func (cc *calleeCtx) constOf(exprs []ast.Expr) constant.Value {
	var val constant.Value
	for _, e := range exprs {
		tv, ok := cc.info.Types[ast.Unparen(e)]
		if !ok || tv.Value == nil || !cc.trustedConst(ast.Unparen(e)) {
			return nil
		}
		if val == nil {
			val = tv.Value
		} else if val.Kind() != tv.Value.Kind() || !constant.Compare(val, token.EQL, tv.Value) {
			return nil
		}
	}
	switch {
	case val == nil:
		return nil
	case val.Kind() == constant.Bool, val.Kind() == constant.String, val.Kind() == constant.Int, val.Kind() == constant.Float:
		return val
	}
	return nil
}

func (cc *calleeCtx) trustedConst(e ast.Expr) bool {
	// Only constants written out in the return itself: a literal, true or
	// false, signs and parentheses. A named constant may be declared in a file
	// built on some systems only (or from runtime.GOOS), a shift or ^ may depend
	// on the word size: those stay unknown, like in the workflow function itself.
	ok := true
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case nil, *ast.BasicLit, *ast.ParenExpr:
		case *ast.UnaryExpr:
			if x.Op != token.SUB && x.Op != token.ADD && x.Op != token.NOT {
				ok = false
			}
		case *ast.Ident:
			if c, isC := cc.info.Uses[x].(*types.Const); !isC || c.Pkg() != nil {
				ok = false // only the predeclared true and false
			}
		default:
			ok = false
		}
		return ok
	})
	return ok
}

// typesOf says which dynamic types an expression may have as an interface
// value: its own static type when that is concrete, what a summarized call
// returns, the types a local variable is given. mayNil says nil is possible.
func (cc *calleeCtx) typesOf(e ast.Expr, visiting map[*types.Var]bool) (ts []types.Type, mayNil, ok bool) {
	e = ast.Unparen(e)
	tv, has := cc.info.Types[e]
	if !has {
		return nil, false, false
	}
	if tv.IsNil() {
		return nil, true, true
	}
	t := tv.Type
	if b, isB := types.Unalias(t).(*types.Basic); isB && b.Info()&types.IsUntyped != 0 {
		t = types.Default(t)
	}
	if concrete(t) {
		return []types.Type{t}, false, true
	}
	if !isInterface(t) {
		return nil, false, false
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		if tvf, isT := cc.info.Types[x.Fun]; isT && tvf.IsType() { // a conversion
			if len(x.Args) == 1 {
				return cc.typesOf(x.Args[0], visiting)
			}
			return nil, false, false
		}
		fn := cc.calleeOf(x)
		if fn == nil {
			return nil, false, false
		}
		sum := cc.a.summarize(fn, cc.depth+1)
		if sum == nil || len(sum.results) != 1 || sum.results[0] == nil || !sum.results[0].typesKnown {
			return nil, false, false
		}
		r := sum.results[0]
		return r.types, r.mayNil, true
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			if at := cc.info.TypeOf(x.Type); at != nil && concrete(at) {
				return []types.Type{at}, false, true
			}
		}
	case *ast.Ident:
		v, isVar := cc.info.Uses[x].(*types.Var)
		if !isVar {
			return nil, false, false
		}
		return cc.varTypes(v, visiting)
	}
	return nil, false, false
}

// varTypes: the types a local interface variable of the function can hold,
// from everything ever assigned to it. A parameter, a variable whose address
// is taken or that a closure touches, and one filled by a form that is not
// understood, are unknown.
func (cc *calleeCtx) varTypes(v *types.Var, visiting map[*types.Var]bool) ([]types.Type, bool, bool) {
	if visiting[v] || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return nil, false, false
	}
	visiting[v] = true
	defer delete(visiting, v)
	body := cc.decl.Body
	if v.Pos() < body.Pos() || v.Pos() >= body.End() {
		return nil, false, false // a parameter or receiver
	}
	e := &keyEntry{known: true}
	bad := false
	declared := false
	add := func(x ast.Expr) {
		ts, mayNil, ok := cc.typesOf(x, visiting)
		if !ok {
			bad = true
			return
		}
		e.addTypes(ts, mayNil, true)
	}
	isV := func(x ast.Expr) bool {
		id, ok := ast.Unparen(x).(*ast.Ident)
		if !ok {
			return false
		}
		return cc.info.Defs[id] == types.Object(v) || cc.info.Uses[id] == types.Object(v)
	}
	var inLit []*ast.FuncLit
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			inLit = append(inLit, x)
			ast.Inspect(x.Body, visit)
			inLit = inLit[:len(inLit)-1]
			return false
		case *ast.Ident:
			if len(inLit) > 0 && (cc.info.Uses[x] == types.Object(v)) {
				bad = true // a closure uses it: it may write it when it runs
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && isV(x.X) {
				bad = true
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if cc.info.Defs[name] != types.Object(v) {
					continue
				}
				declared = true
				switch {
				case len(x.Values) == len(x.Names):
					add(x.Values[i])
				case len(x.Values) == 0:
					e.mayNil = true // the zero value of an interface is nil
				default:
					bad = true
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if !isV(l) {
					continue
				}
				if x.Tok == token.DEFINE && cc.info.Defs[ast.Unparen(l).(*ast.Ident)] == types.Object(v) {
					declared = true
				}
				switch {
				case (x.Tok == token.ASSIGN || x.Tok == token.DEFINE) && len(x.Lhs) == len(x.Rhs):
					add(x.Rhs[i])
				default:
					bad = true
				}
			}
		case *ast.IncDecStmt:
			if isV(x.X) {
				bad = true
			}
		case *ast.RangeStmt:
			if (x.Key != nil && isV(x.Key)) || (x.Value != nil && isV(x.Value)) {
				bad = true
			}
		}
		return !bad
	}
	ast.Inspect(body, visit)
	if bad || !declared || !e.known {
		return nil, false, false
	}
	return e.types, e.mayNil, true
}

// mapResult: every return gives the same local map variable (or nil), built
// here and never handed on. It returns, per constant string key, what is
// stored there.
func (cc *calleeCtx) mapResult(exprs []ast.Expr) (map[string]*keyEntry, []string, bool) {
	var mv *types.Var
	for _, e := range exprs {
		e = ast.Unparen(e)
		if tv, ok := cc.info.Types[e]; ok && tv.IsNil() {
			continue
		}
		id, ok := e.(*ast.Ident)
		if !ok {
			return nil, nil, false
		}
		v, ok := cc.info.Uses[id].(*types.Var)
		if !ok || (mv != nil && mv != v) {
			return nil, nil, false
		}
		mv = v
	}
	if mv == nil {
		return nil, nil, false
	}
	body := cc.decl.Body
	if mv.IsField() || mv.Pkg() == nil || mv.Parent() == mv.Pkg().Scope() || mv.Pos() < body.Pos() || mv.Pos() >= body.End() {
		return nil, nil, false
	}
	per := map[string]*keyEntry{}
	put := func(key string, valExpr ast.Expr) {
		ent := per[key]
		if ent == nil {
			ent = &keyEntry{known: true}
			per[key] = ent
		}
		ts, mayNil, ok := cc.typesOf(valExpr, map[*types.Var]bool{})
		ent.addTypes(ts, mayNil, ok)
	}
	constKey := func(k ast.Expr) (string, bool) {
		_, isLit := ast.Unparen(k).(*ast.BasicLit)
		tv, ok := cc.info.Types[ast.Unparen(k)]
		if !isLit || !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return "", false // a named constant may differ between builds
		}
		return constant.StringVal(tv.Value), true
	}
	isMV := func(x ast.Expr) bool {
		id, ok := ast.Unparen(x).(*ast.Ident)
		return ok && (cc.info.Uses[id] == types.Object(mv) || cc.info.Defs[id] == types.Object(mv))
	}
	okLits := cc.closureWriters(mv, per, put, constKey)
	defs := 0
	bad := false
	// allowed marks the identifier occurrences that are understood uses.
	allowed := map[*ast.Ident]bool{}
	var inLit int
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		if bad {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			if okLits[x] {
				return false // a writer closure that was read whole (closureWriters)
			}
			inLit++
			ast.Inspect(x.Body, visit)
			inLit--
			return false
		case *ast.ReturnStmt:
			for _, r := range x.Results {
				if id, ok := ast.Unparen(r).(*ast.Ident); ok && isMV(id) {
					allowed[id] = true
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if ix, ok := ast.Unparen(l).(*ast.IndexExpr); ok && isMV(ix.X) { // v[k] = e
					key, isConst := constKey(ix.Index)
					if !isConst || x.Tok != token.ASSIGN || len(x.Lhs) != len(x.Rhs) || inLit > 0 {
						bad = true
						return false
					}
					allowed[ast.Unparen(ix.X).(*ast.Ident)] = true
					put(key, x.Rhs[i])
					continue
				}
				if isMV(l) { // v := map...{} / v = ...
					defs++
					if x.Tok != token.DEFINE || len(x.Lhs) != len(x.Rhs) || inLit > 0 {
						bad = true
						return false
					}
					if !cc.mapInit(x.Rhs[i], per, put, constKey) {
						bad = true
						return false
					}
					allowed[ast.Unparen(l).(*ast.Ident)] = true
				}
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if cc.info.Defs[name] != types.Object(mv) {
					continue
				}
				defs++
				if inLit > 0 || len(x.Values) != len(x.Names) || !cc.mapInit(x.Values[i], per, put, constKey) {
					bad = true
					return false
				}
				allowed[name] = true
			}
		case *ast.IndexExpr: // a read v[k]
			if isMV(x.X) {
				allowed[ast.Unparen(x.X).(*ast.Ident)] = true
			}
		case *ast.CallExpr: // len(v)
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				if b, isB := cc.info.Uses[id].(*types.Builtin); isB && b.Name() == "len" && len(x.Args) == 1 && isMV(x.Args[0]) {
					allowed[ast.Unparen(x.Args[0]).(*ast.Ident)] = true
				}
			}
		case *ast.RangeStmt: // for ... range v: reads only
			if isMV(x.X) {
				allowed[ast.Unparen(x.X).(*ast.Ident)] = true
			}
		}
		return true
	}
	ast.Inspect(body, visit)
	if bad || defs != 1 {
		return nil, nil, false
	}
	// Any other mention of the map variable lets something else reach it.
	other := false
	ast.Inspect(body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok && okLits[fl] {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && (cc.info.Uses[id] == types.Object(mv) || cc.info.Defs[id] == types.Object(mv)) && !allowed[id] {
			other = true
		}
		return !other
	})
	if other {
		return nil, nil, false
	}
	// A write in a range loop's body to a map that is also ranged over is fine
	// for the types; a use inside a closure is not (it was refused above only for
	// writes), so refuse every mention inside one.
	var inClosure bool
	ast.Inspect(body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok && !okLits[fl] {
			ast.Inspect(fl.Body, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && cc.info.Uses[id] == types.Object(mv) {
					inClosure = true
				}
				return !inClosure
			})
			return false
		}
		return !inClosure
	})
	if inClosure {
		return nil, nil, false
	}
	order := make([]string, 0, len(per))
	for k := range per {
		order = append(order, k)
	}
	sort.Strings(order)
	return per, order, true
}

// mapInit reads the first value of the map variable: a literal with constant
// string keys, or make(map...).
func (cc *calleeCtx) mapInit(e ast.Expr, per map[string]*keyEntry, put func(string, ast.Expr), constKey func(ast.Expr) (string, bool)) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				return false
			}
			key, isConst := constKey(kv.Key)
			if !isConst {
				return false
			}
			put(key, kv.Value)
		}
		return true
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if b, isB := cc.info.Uses[id].(*types.Builtin); isB && b.Name() == "make" {
				return true
			}
		}
	}
	return false
}

// buildDependent: the file is only built on some systems or with some tags, so
// the function PathKit reads here may not be the one that runs elsewhere.
func buildDependent(f *ast.File, filename string) bool {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
				return true
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(filename), ".go")
	parts := strings.Split(base, "_")
	for i := len(parts) - 1; i >= 1 && i >= len(parts)-2; i-- {
		if knownSystem[parts[i]] {
			return true
		}
	}
	return false
}

var knownSystem = map[string]bool{}

func init() {
	for _, n := range strings.Fields("aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos 386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm") {
		knownSystem[n] = true
	}
}

// closureWriters reads the shape
//
//	add := func(key string, val *string) { if val != nil { m[key] = *val } }
//	add("A", x); add("B", y)
//
// where a function literal kept in a variable is the only code that writes the
// map through a name it was given: every mention of the map in the literal is a
// store `m[p] = e` with p one of its parameters, p is never assigned in it, and
// every use of the variable is a call whose argument for p is a constant text.
// The map then holds, under each of those constant texts, what the stores can
// give (their static types). It returns the literals it read whole.
func (cc *calleeCtx) closureWriters(mv *types.Var, per map[string]*keyEntry, put func(string, ast.Expr), constKey func(ast.Expr) (string, bool)) map[*ast.FuncLit]bool {
	ok := map[*ast.FuncLit]bool{}
	type writer struct {
		lit     *ast.FuncLit
		keyPos  int
		stores  []ast.Expr
		invalid bool
	}
	writers := map[types.Object]*writer{}
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		as, isAs := n.(*ast.AssignStmt)
		if !isAs || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lit, isLit := ast.Unparen(as.Rhs[0]).(*ast.FuncLit)
		fid, isID := as.Lhs[0].(*ast.Ident)
		if !isLit || !isID {
			return true
		}
		obj := cc.info.Defs[fid]
		if obj == nil {
			return true
		}
		w := &writer{lit: lit, keyPos: -1}
		params := map[types.Object]int{}
		i := 0
		for _, f := range lit.Type.Params.List {
			names := f.Names
			if len(names) == 0 {
				i++
				continue
			}
			for _, nm := range names {
				if po := cc.info.Defs[nm]; po != nil {
					params[po] = i
				}
				i++
			}
		}
		uses := false
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.Ident:
				if cc.info.Uses[x] == types.Object(mv) {
					uses = true
				}
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok {
						if _, isParam := params[cc.info.Uses[id]]; isParam {
							w.invalid = true // a parameter is assigned
						}
					}
				}
			case *ast.FuncLit:
				if x != lit {
					w.invalid = true // a closure inside: not followed
				}
			}
			return !w.invalid
		})
		if !uses || w.invalid {
			return true // no use of the map, or not understood: left to the general rules
		}
		// every mention of the map must be a store through a parameter
		stores := 0
		mentions := 0
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.Ident:
				if cc.info.Uses[x] == types.Object(mv) {
					mentions++
				}
			case *ast.AssignStmt:
				if x.Tok != token.ASSIGN || len(x.Lhs) != 1 || len(x.Rhs) != 1 {
					return true
				}
				ix, isIx := ast.Unparen(x.Lhs[0]).(*ast.IndexExpr)
				if !isIx {
					return true
				}
				base, isBase := ast.Unparen(ix.X).(*ast.Ident)
				if !isBase || cc.info.Uses[base] != types.Object(mv) {
					return true
				}
				kid, isKey := ast.Unparen(ix.Index).(*ast.Ident)
				if !isKey {
					return true
				}
				pi, isParam := params[cc.info.Uses[kid]]
				if !isParam || (w.keyPos >= 0 && w.keyPos != pi) {
					return true
				}
				w.keyPos = pi
				w.stores = append(w.stores, x.Rhs[0])
				stores++
			}
			return true
		})
		if stores == 0 || stores != mentions {
			return true
		}
		writers[obj] = w
		return true
	})
	if len(writers) == 0 {
		return ok
	}
	// every use of the variable must be a call with a constant text for the key
	calls := map[types.Object][]string{}
	badUse := map[types.Object]bool{}
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, isID := ast.Unparen(x.Fun).(*ast.Ident); isID {
				if w := writers[cc.info.Uses[id]]; w != nil {
					if w.keyPos < len(x.Args) {
						if k, isConst := constKey(x.Args[w.keyPos]); isConst {
							calls[cc.info.Uses[id]] = append(calls[cc.info.Uses[id]], k)
							return true
						}
					}
					badUse[cc.info.Uses[id]] = true
				}
			}
		}
		return true
	})
	// a mention of the variable that is not the function of a call is a use too
	callFuns := map[*ast.Ident]bool{}
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		if c, isCall := n.(*ast.CallExpr); isCall {
			if id, isID := ast.Unparen(c.Fun).(*ast.Ident); isID {
				callFuns[id] = true
			}
		}
		return true
	})
	ast.Inspect(cc.decl.Body, func(n ast.Node) bool {
		if id, isID := n.(*ast.Ident); isID {
			if o := cc.info.Uses[id]; writers[o] != nil && !callFuns[id] {
				badUse[o] = true
			}
		}
		return true
	})
	for obj, w := range writers {
		if badUse[obj] {
			continue
		}
		for _, k := range calls[obj] {
			for _, st := range w.stores {
				put(k, st)
			}
		}
		ok[w.lit] = true
	}
	return ok
}
