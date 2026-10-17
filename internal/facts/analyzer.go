package facts

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Options are the assumptions the analyzer may rest claims on.
type Options struct {
	// JSONDecode: the module uses Temporal's default JSON converter, so a
	// value decoded into an interface{} holds only string, float64, bool,
	// nil, []interface{} or map[string]interface{} (assumption
	// default-json-converter). False turns the rule off.
	JSONDecode bool
}

// Analyzer analyzes the paths of one workflow.
type Analyzer struct {
	opts Options
	g    *model.Graph
	info *types.Info
	fset *token.FileSet
	// off is why the engine does nothing for this function ("" when on).
	off string
	// untracked are variables the engine never keeps facts about, with the
	// reason in plain words.
	untracked map[*types.Var]string
	// assignedIn caches the variables assigned anywhere inside a statement.
	assignedIn map[ast.Node]map[*types.Var]bool
	// calls remembers, per node, whether it holds a call (see mayCall).
	calls map[ast.Node]bool
	// escapes are slices and maps used in a way that lets something else
	// reach their elements or length (passed to a call, copied, captured by a
	// closure, deleted from, ...); nothing is known about their contents.
	escapes map[*types.Var]string
	// decode are the calls that fill a variable through its address with a
	// value Temporal decoded (Future.Get, ReceiveChannel.Receive, ...), and
	// the variable they fill. Those addresses do not make a variable
	// untracked: the call is the only place it is written through.
	decode map[*ast.CallExpr]*types.Var
	// decodeStd marks the calls in decode that encoding/json makes (json.Unmarshal,
	// Decode on a decoder this function made and never switched to UseNumber).
	decodeStd map[*ast.CallExpr]bool
	// useNumber are the decoder variables UseNumber is called on.
	useNumber map[*types.Var]bool
	bodies    []*ast.FuncDecl
	// useNumberUnknown: UseNumber is called on something that is not a plain variable.
	useNumberUnknown bool
	// addrTaken counts the & expressions on each variable.
	addrTaken map[*types.Var]int
	// jsonWrite: the function writes into some map[string]interface{} or
	// []interface{}; then nothing is assumed about the contents of decoded
	// containers (another name for one may have stored a different type).
	jsonWrite bool
	// ifaceEscapes are interface variables used in a way that lets something
	// else reach the value they hold (passed on, copied, captured): a map or
	// slice taken out of one by a type assertion is then not followed.
	ifaceEscapes map[*types.Var]string
	// assertDerived are variables that a type assertion or a type switch
	// case gave a value: they share the container inside the interface.
	assertDerived map[*types.Var]bool
	// inits are the values a variable is given; a variable with exactly one
	// is "set once" (see singleInit).
	inits map[*types.Var][]ast.Expr
}

// New prepares the engine for one workflow's graph.
func New(g *model.Graph, opts ...Options) *Analyzer {
	a := &Analyzer{g: g, info: g.TypesInfo(), fset: g.FileSet(), untracked: map[*types.Var]string{}, assignedIn: map[ast.Node]map[*types.Var]bool{}, calls: map[ast.Node]bool{},
		escapes: map[*types.Var]string{}, ifaceEscapes: map[*types.Var]string{}, assertDerived: map[*types.Var]bool{}, inits: map[*types.Var][]ast.Expr{}, decode: map[*ast.CallExpr]*types.Var{}, decodeStd: map[*ast.CallExpr]bool{}, useNumber: map[*types.Var]bool{}, addrTaken: map[*types.Var]int{}}
	if len(opts) > 0 {
		a.opts = opts[0]
	}
	if a.info == nil || g.Func() == nil || g.Func().Body == nil {
		a.off = "the code is not available"
		return a
	}
	// The workflow function, and the declared helpers whose code the graph
	// follows into: both are code the paths run.
	fns := []*ast.FuncDecl{g.Func()}
	seen := map[*ast.FuncDecl]bool{}
	for _, s := range g.Sites() {
		if s.Decl != nil && !seen[s.Decl] {
			seen[s.Decl] = true
			fns = append(fns, s.Decl)
		}
	}
	a.scan(fns)
	a.findEscapes(fns)
	return a
}

// JSONRelevant reports whether the function has any value Temporal decodes
// into an interface{}, map[string]interface{} or []interface{}: a workflow
// input of that type, or a variable filled by Get / Receive. Only then does
// the assumption default-json-converter matter to this function.
func (a *Analyzer) JSONRelevant() bool {
	if len(a.decode) > 0 {
		return true
	}
	if fn := a.g.Func(); fn != nil && fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			if t := a.info.TypeOf(f.Type); t != nil && decodable(t) {
				return true
			}
		}
	}
	return false
}

// Off returns why the engine is switched off for this function, or "".
func (a *Analyzer) Off() string { return a.off }

// rootVar is the variable at the bottom of an addressable expression:
// x, x.f, x.f.g, x[i], (x), *x ... (nil when there is none).
func (a *Analyzer) rootVar(e ast.Expr) *types.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if sel := a.info.Selections[x]; sel == nil {
				// pkg.Name: a package-level variable, never tracked.
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.Ident:
			v, _ := a.info.ObjectOf(x).(*types.Var)
			return v
		default:
			return nil
		}
	}
}

func (a *Analyzer) markUntracked(e ast.Expr, why string) {
	if v := a.rootVar(e); v != nil {
		if _, seen := a.untracked[v]; !seen {
			a.untracked[v] = why
		}
	}
}

// scan finds everything that makes the function unsuitable, and every
// variable that must never be tracked (the safety list).
func (a *Analyzer) scan(fns []*ast.FuncDecl) {
	a.bodies = fns
	var lits []*ast.FuncLit // the function literals around the node being visited
	var cands, stdCands []*ast.CallExpr
	var visit func(n ast.Node) bool
	// writtenInClosure marks v untracked when the write happens inside a
	// function literal that v is declared outside of.
	writtenInClosure := func(lhs ast.Expr) {
		if len(lits) == 0 {
			return
		}
		v := a.rootVar(lhs)
		if v == nil {
			return
		}
		inner := lits[len(lits)-1]
		if v.Pos() < inner.Pos() || v.Pos() >= inner.End() { // declared outside the closure
			if _, seen := a.untracked[v]; !seen {
				a.untracked[v] = "it is written inside a closure (a function literal)"
			}
		}
	}
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			lits = append(lits, x)
			ast.Inspect(x.Body, visit)
			lits = lits[:len(lits)-1]
			return false
		case *ast.BranchStmt:
			if x.Tok == token.GOTO {
				a.off = "it uses goto"
			}
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				if b, ok := a.info.Uses[id].(*types.Builtin); ok {
					switch b.Name() {
					case "recover":
						a.off = "it calls recover"
					case "delete", "clear", "copy", "append":
						if len(x.Args) > 0 && isJSONContainer(a.info.TypeOf(x.Args[0])) {
							a.jsonWrite = true
						}
					}
				}
			}
			if len(lits) == 0 && a.isDecodeShape(x) {
				cands = append(cands, x) // decided once the whole function has been read (see below)
			}
			if len(lits) == 0 && a.isStdDecodeShape(x) {
				stdCands = append(stdCands, x)
			}
			a.noteUseNumber(x)
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				if pn, ok := a.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "unsafe" {
					a.off = "it uses the unsafe package"
				}
			}
			// A method with a pointer receiver called on a value takes its
			// address without writing &.
			if sel := a.info.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
				if sig, ok := sel.Obj().Type().(*types.Signature); ok && sig.Recv() != nil {
					if _, ptrRecv := sig.Recv().Type().Underlying().(*types.Pointer); ptrRecv {
						if tv, ok := a.info.Types[x.X]; ok {
							if _, isPtr := tv.Type.Underlying().(*types.Pointer); !isPtr {
								a.markUntracked(x.X, "a method with a pointer receiver is called on it (Go takes its address)")
							}
						}
					}
				}
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				if v := a.rootVar(x.X); v != nil {
					a.addrTaken[v]++
				}
			}
		case *ast.SliceExpr:
			if tv, ok := a.info.Types[x.X]; ok {
				if _, isArr := tv.Type.Underlying().(*types.Array); isArr {
					a.markUntracked(x.X, "a slice is taken of it (Go takes its address)")
				}
			}
		case *ast.TypeSwitchStmt:
			for _, c := range x.Body.List {
				if v, ok := a.info.Implicits[c].(*types.Var); ok {
					a.assertDerived[v] = true
				}
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				v, _ := a.info.Defs[name].(*types.Var)
				if v == nil {
					continue
				}
				switch {
				case len(x.Values) == len(x.Names):
					a.inits[v] = append(a.inits[v], x.Values[i])
					a.noteAssert(v, x.Values[i])
				default:
					a.inits[v] = append(a.inits[v], nil) // zero value, or several results of one call
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				id, isIdent := ast.Unparen(l).(*ast.Ident)
				if !isIdent {
					continue
				}
				v, _ := a.info.ObjectOf(id).(*types.Var)
				if v == nil {
					continue
				}
				switch {
				case len(x.Lhs) == len(x.Rhs) && x.Tok == token.DEFINE || len(x.Lhs) == len(x.Rhs) && x.Tok == token.ASSIGN:
					a.inits[v] = append(a.inits[v], x.Rhs[i])
					a.noteAssert(v, x.Rhs[i])
				case len(x.Rhs) == 1 && i == 0:
					a.inits[v] = append(a.inits[v], nil)
					a.noteAssert(v, x.Rhs[0]) // v, ok := x.(T)
				default:
					a.inits[v] = append(a.inits[v], nil)
				}
			}
			for _, l := range x.Lhs {
				writtenInClosure(l)
				if ix, ok := ast.Unparen(l).(*ast.IndexExpr); ok && isJSONContainer(a.info.TypeOf(ix.X)) {
					a.jsonWrite = true
				}
			}
		case *ast.IncDecStmt:
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
				if v, _ := a.info.ObjectOf(id).(*types.Var); v != nil {
					a.inits[v] = append(a.inits[v], nil)
				}
			}
			writtenInClosure(x.X)
			if ix, ok := ast.Unparen(x.X).(*ast.IndexExpr); ok && isJSONContainer(a.info.TypeOf(ix.X)) {
				a.jsonWrite = true
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{x.Key, x.Value} {
				if id, ok := e.(*ast.Ident); ok {
					if v, _ := a.info.ObjectOf(id).(*types.Var); v != nil {
						a.inits[v] = append(a.inits[v], nil)
					}
				}
			}
			if x.Key != nil {
				writtenInClosure(x.Key)
			}
			if x.Value != nil {
				writtenInClosure(x.Value)
			}
		}
		return true
	}
	for _, fn := range fns {
		ast.Inspect(fn.Body, visit)
	}
	// The calls that fill a variable through its address with a value
	// Temporal decoded: now that every assignment is known, check that the
	// future or channel really comes from Temporal's payloads.
	decoded := map[*types.Var]int{}
	for _, call := range cands {
		if !a.isDecodeCall(call) {
			continue
		}
		for _, arg := range call.Args {
			u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				continue
			}
			id, ok := ast.Unparen(u.X).(*ast.Ident)
			if !ok {
				continue
			}
			if v, ok := a.info.ObjectOf(id).(*types.Var); ok && decodable(v.Type()) {
				a.decode[call] = v
				decoded[v]++
			}
		}
	}
	// The same for encoding/json in this function: json.Unmarshal, and Decode
	// on a decoder made here by json.NewDecoder that nothing hands on and
	// nothing switches to UseNumber.
	for _, call := range stdCands {
		if !a.isStdDecodeCall(call) {
			continue
		}
		for _, arg := range call.Args {
			u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				continue
			}
			id, ok := ast.Unparen(u.X).(*ast.Ident)
			if !ok {
				continue
			}
			if v, ok := a.info.ObjectOf(id).(*types.Var); ok && decodable(v.Type()) {
				a.decode[call] = v
				a.decodeStd[call] = true
				decoded[v]++
			}
		}
	}
	// An address is an escape unless every one of them is a decode call.
	for v, n := range a.addrTaken {
		if n > decoded[v] {
			if _, seen := a.untracked[v]; !seen {
				a.untracked[v] = "its address is taken (&)"
			}
		}
	}
}

// trackable reports whether v may carry facts: a local or parameter of the
// workflow function that none of the safety-list rules excludes.
func (a *Analyzer) trackable(v *types.Var) bool {
	if v == nil || v.IsField() || v.Pkg() == nil {
		return false
	}
	if v.Parent() == v.Pkg().Scope() { // package-level
		return false
	}
	_, bad := a.untracked[v]
	return !bad
}

// assignedVars returns the variables assigned anywhere inside stmt
// (including inside closures and nested statements).
func (a *Analyzer) assignedVars(stmt ast.Node) map[*types.Var]bool {
	if m, ok := a.assignedIn[stmt]; ok {
		return m
	}
	m := map[*types.Var]bool{}
	add := func(e ast.Expr) {
		if e == nil {
			return
		}
		if v := a.rootVar(e); v != nil {
			m[v] = true
		}
	}
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if v, ok := a.decode[x]; ok {
				m[v] = true // Get(ctx, &v) writes v
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				add(l)
			}
		case *ast.IncDecStmt:
			add(x.X)
		case *ast.RangeStmt:
			add(x.Key)
			add(x.Value)
		case *ast.ValueSpec:
			for _, name := range x.Names {
				if v, ok := a.info.Defs[name].(*types.Var); ok {
					m[v] = true
				}
			}
		}
		return true
	})
	a.assignedIn[stmt] = m
	return m
}

func (a *Analyzer) baseFile(pos token.Pos) (string, int) {
	p := a.fset.Position(pos)
	return filepath.Base(p.Filename), p.Line
}

// isJSONContainer: map[string]interface{} or []interface{}, the containers
// Temporal's JSON converter fills.
func isJSONContainer(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Map:
		k, ok := u.Key().Underlying().(*types.Basic)
		return ok && k.Kind() == types.String && isEmptyInterface(u.Elem())
	case *types.Slice:
		return isEmptyInterface(u.Elem())
	}
	return false
}

// decodable: a variable type whose decoded value the engine knows
// something about (an interface{}, map[string]interface{} or []interface{}).
func decodable(t types.Type) bool { return isEmptyInterface(t) || isJSONContainer(t) }

// isDecodeShape: a call that looks like one of Temporal's methods that decode
// a value into a pointer (workflow.Future.Get, ReceiveChannel.Receive, the
// SideEffect result's Get, ...), without checking where the future or channel
// comes from (see isDecodeCall).
func (a *Analyzer) isDecodeShape(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Get", "Receive", "ReceiveAsync", "ReceiveAsyncWithMoreFlag", "ReceiveWithTimeout":
	default:
		return false
	}
	s := a.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false
	}
	recv := types.Unalias(s.Recv())
	if p, ok := recv.(*types.Pointer); ok {
		recv = types.Unalias(p.Elem())
	}
	named, ok := recv.(*types.Named)
	return ok && named.Obj().Pkg() != nil && strings.HasPrefix(named.Obj().Pkg().Path(), "go.temporal.io/sdk")
}

// isDecodeCall: isDecodeShape, and the future, channel or side-effect result
// is one whose values come out of the data converter.
func (a *Analyzer) isDecodeCall(call *ast.CallExpr) bool {
	if !a.isDecodeShape(call) {
		return false
	}
	sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return a.payloadSource(sel.X, 0)
}

// findEscapes decides, for every slice or map variable, whether the function
// uses it in a way that lets something else reach its elements or length.
// Only a variable that never escapes may carry facts about its contents.
func (a *Analyzer) findEscapes(fns []*ast.FuncDecl) {
	for _, fn := range fns {
		a.escapesIn(fn)
	}
	// A container that a type assertion took out of a decoded value is the
	// same container the interface holds. If any such name is passed on or
	// captured, something else may write into it, and no container found in a
	// decoded value is followed.
	for v := range a.assertDerived {
		if isJSONContainer(v.Type()) && a.escapes[v] != "" {
			a.jsonWrite = true
		}
	}
}

func (a *Analyzer) escapesIn(fn *ast.FuncDecl) {
	var stack []ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := a.info.Uses[id].(*types.Var); ok && !v.IsField() {
				if t := v.Type(); t != nil {
					switch t.Underlying().(type) {
					case *types.Map, *types.Slice:
						if _, seen := a.escapes[v]; !seen {
							if why := a.escapeUse(id, v, stack); why != "" {
								a.escapes[v] = why
							}
						}
					}
					if isInterface(t) {
						if _, seen := a.ifaceEscapes[v]; !seen {
							if why := a.ifaceEscapeUse(id, stack); why != "" {
								a.ifaceEscapes[v] = why
							}
						}
					}
				}
			}
		}
		stack = append(stack, n)
		return true
	})
}

// ifaceEscapeUse says why this use of the interface variable lets something
// else reach the value it holds ("" when the use is harmless): only a type
// assertion on it, a comparison with nil, or giving it a new value.
func (a *Analyzer) ifaceEscapeUse(id *ast.Ident, stack []ast.Node) string {
	for _, n := range stack {
		if _, ok := n.(*ast.FuncLit); ok {
			return "a closure uses it"
		}
	}
	if len(stack) == 0 {
		return "used outside a statement"
	}
	switch p := stack[len(stack)-1].(type) {
	case *ast.TypeAssertExpr:
		if p.X == ast.Expr(id) {
			return ""
		}
	case *ast.BinaryExpr:
		if p.Op == token.EQL || p.Op == token.NEQ {
			other := p.X
			if other == ast.Expr(id) {
				other = p.Y
			}
			if tv, ok := a.info.Types[other]; ok && tv.IsNil() {
				return ""
			}
		}
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == ast.Expr(id) {
				return ""
			}
		}
	}
	return "it is passed on, copied or stored where PathKit does not follow it"
}

// escapeUse says why this use of the slice or map v lets something else reach
// it ("" when the use is harmless).
func (a *Analyzer) escapeUse(id *ast.Ident, v *types.Var, stack []ast.Node) string {
	for _, n := range stack {
		if _, ok := n.(*ast.FuncLit); ok {
			return "a closure uses it"
		}
	}
	if len(stack) == 0 {
		return "used outside a statement"
	}
	parent := stack[len(stack)-1]
	var grand ast.Node
	if len(stack) > 1 {
		grand = stack[len(stack)-2]
	}
	const other = "it is passed on, copied or stored where PathKit does not follow it"
	switch p := parent.(type) {
	case *ast.CallExpr:
		if f, ok := ast.Unparen(p.Fun).(*ast.Ident); ok {
			if b, ok := a.info.Uses[f].(*types.Builtin); ok && len(p.Args) > 0 && p.Args[0] == ast.Expr(id) {
				switch b.Name() {
				case "len", "cap":
					return ""
				case "append":
					if as, ok := grand.(*ast.AssignStmt); ok && as.Tok == token.ASSIGN && len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Rhs[0] == ast.Expr(p) {
						if l, ok := ast.Unparen(as.Lhs[0]).(*ast.Ident); ok && a.info.ObjectOf(l) == types.Object(v) {
							return ""
						}
					}
				}
			}
		}
		return other
	case *ast.IndexExpr:
		if p.X == ast.Expr(id) {
			return ""
		}
	case *ast.RangeStmt:
		if p.X == ast.Expr(id) {
			return ""
		}
	case *ast.BinaryExpr:
		if p.Op == token.EQL || p.Op == token.NEQ {
			other := p.X
			if other == ast.Expr(id) {
				other = p.Y
			}
			if tv, ok := a.info.Types[other]; ok && tv.IsNil() {
				return ""
			}
		}
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == ast.Expr(id) {
				return ""
			}
		}
	}
	return other
}

// noteAssert records that v was given the value of a type assertion.
func (a *Analyzer) noteAssert(v *types.Var, rhs ast.Expr) {
	if _, ok := ast.Unparen(rhs).(*ast.TypeAssertExpr); ok {
		a.assertDerived[v] = true
	}
}

// singleInit returns the one value a variable is ever given (":=" or "var
// x = ..."), and false when it is given more than one, or is changed in
// some other way (including by taking its address).
func (a *Analyzer) singleInit(v *types.Var) (ast.Expr, bool) {
	if _, bad := a.untracked[v]; bad || v == nil {
		return nil, false
	}
	in := a.inits[v]
	if len(in) != 1 || in[0] == nil {
		return nil, false
	}
	return in[0], true
}

// payloadSource reports whether e is a Temporal object whose values come
// out of the data converter: the future of an activity, a local activity,
// a child workflow or an external signal; a signal channel; the result of
// SideEffect or MutableSideEffect. A channel or future the workflow makes
// itself (NewChannel, NewFuture) hands over Go values untouched, so it is
// not one.
func (a *Analyzer) payloadSource(e ast.Expr, depth int) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		var obj types.Object
		switch f := ast.Unparen(x.Fun).(type) {
		case *ast.Ident:
			obj = a.info.Uses[f]
		case *ast.SelectorExpr:
			obj = a.info.Uses[f.Sel]
		}
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "go.temporal.io/sdk/workflow" {
			return false
		}
		switch fn.Name() {
		case "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow", "SignalExternalWorkflow",
			"GetSignalChannel", "GetSignalChannelWithOptions", "SideEffect", "MutableSideEffect":
			return true
		}
	case *ast.Ident:
		if v, ok := a.info.Uses[x].(*types.Var); ok && depth < 3 {
			if init, ok := a.singleInit(v); ok {
				return a.payloadSource(init, depth+1)
			}
		}
	}
	return false
}

// jsonFunc reports whether call is a call of the function or method name of
// the standard library's encoding/json (recv "" for a function, else the
// type's name).
func (a *Analyzer) jsonFunc(call *ast.CallExpr, recv, name string) bool {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return false
	}
	fn, ok := a.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "encoding/json" || fn.Name() != name {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if recv == "" {
		return sig.Recv() == nil
	}
	if sig.Recv() == nil {
		return false
	}
	t := types.Unalias(sig.Recv().Type())
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == recv
}

// isStdDecodeShape: json.Unmarshal(data, &v) or dec.Decode(&v).
func (a *Analyzer) isStdDecodeShape(call *ast.CallExpr) bool {
	return a.jsonFunc(call, "", "Unmarshal") || a.jsonFunc(call, "Decoder", "Decode")
}

// isStdDecodeCall: isStdDecodeShape, and nothing makes the result differ
// from the standard library's rule (a decoder is one made here with
// json.NewDecoder, that is only used through its own methods, and that
// UseNumber is never called on).
func (a *Analyzer) isStdDecodeCall(call *ast.CallExpr) bool {
	if a.jsonFunc(call, "", "Unmarshal") {
		return true
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := a.info.Uses[id].(*types.Var)
	if !ok || a.useNumber[v] {
		return false
	}
	init, ok := a.singleInit(v)
	if !ok {
		return false
	}
	nd, ok := ast.Unparen(init).(*ast.CallExpr)
	if !ok || !a.jsonFunc(nd, "", "NewDecoder") {
		return false
	}
	return a.onlyUsedAsReceiver(v)
}

// noteUseNumber remembers a call of UseNumber on a decoder variable. Any
// other form (the decoder is not a plain variable) is remembered for every
// decoder: nothing is then known about any of them.
func (a *Analyzer) noteUseNumber(call *ast.CallExpr) {
	if !a.jsonFunc(call, "Decoder", "UseNumber") {
		return
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
			if v, ok := a.info.Uses[id].(*types.Var); ok {
				a.useNumber[v] = true
				return
			}
		}
	}
	a.useNumberUnknown = true
}

// onlyUsedAsReceiver: every use of v in the code the paths run is a method
// call on it (dec.Decode(...)), so nothing else can change the decoder.
func (a *Analyzer) onlyUsedAsReceiver(v *types.Var) bool {
	if a.useNumberUnknown {
		return false
	}
	total, recv := 0, 0
	for _, fn := range a.bodies {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if a.info.Uses[x] == v {
					total++
				}
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && a.info.Uses[id] == v {
					recv++
				}
			}
			return true
		})
	}
	return total == recv
}
