package model

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
)

// Kinds of Unread entry: the places PathKit treated something as unknown or
// did not follow it (the "PathKit could not fully read" panel).
const (
	// UnreadInterfaceCall: a method called through an interface; any type could
	// be behind it.
	UnreadInterfaceCall = "interface method call"
	// UnreadFuncValue: a call through a function value (a variable, a field or
	// a parameter) that PathKit cannot trace to one function.
	UnreadFuncValue = "function value call"
	// UnreadOtherPackage: a function of another package of the same module.
	UnreadOtherPackage = "function in another package"
	// UnreadOtherModule: a function of another module (a library).
	UnreadOtherModule = "function from another module"
	// UnreadHelper: a helper of the workflow's own package that does workflow
	// work but is not followed.
	UnreadHelper = "helper not followed"
	// UnreadByName: an activity or workflow started by a name that is not a
	// fixed text.
	UnreadByName = "started by a computed name"
	// UnreadFutureNeverRead: a future whose result is never read.
	UnreadFutureNeverRead = "result never read"
	// UnreadReturnedCall: a function returns the error of a Temporal call
	// directly, so its failure and its success are one path.
	UnreadReturnedCall = "result returned directly"
	// UnreadClosure: a function literal that calls the Temporal SDK and is not
	// one PathKit maps (a selector callback, an update or query handler, a
	// deferred compensation): its calls are not steps of any path.
	UnreadClosure = "closure not mapped"
	// UnreadValidatorPanic: an update validator that rejects by panicking; the
	// panic is not a path, so the rejection is missing from its map.
	UnreadValidatorPanic = "validator rejects by panic"
)

// Unread is one place PathKit did not (fully) read.
type Unread struct {
	Workflow string
	File     string // as the source knows it; the caller shortens it
	Line     int
	Kind     string
	// Call is the code, "s.store.Save(ctx, o)".
	Call string
	// Effect says what PathKit did with it, in plain words.
	Effect string
	// Suggestion, for a function that could be told about with
	// classification.activity_wrappers: the entry to add (empty if none).
	Suggestion string
}

// Unread lists, in source order, the calls that take a workflow.Context
// that PathKit did not follow into, and the other places it treated as
// unknown (docs/DESIGN.md D5 on one source of truth: the model decides what
// it follows, so the model says what it did not).
func (g *Graph) Unread() []Unread { return g.unread }

// collectUnread scans the workflow function and every copy of helper code
// for calls that take a workflow.Context and are not followed.
func (b *builder) collectUnread() []Unread {
	followed := map[*ast.CallExpr]bool{}
	for _, s := range b.siteList {
		followed[s.Call] = true
	}
	for _, u := range b.units {
		for _, c := range u.Calls {
			followed[c.Call] = true // read as a unit
		}
	}
	type scan struct {
		body  *ast.BlockStmt
		depth int // how many helpers deep the code is: 0 for the workflow function
		typ   *ast.FuncType
	}
	scans := []scan{{b.wf.Func.Body, 0, b.wf.Func.Type}}
	seen := map[*ast.BlockStmt]bool{b.wf.Func.Body: true}
	for _, s := range b.siteList {
		if !seen[s.Body] {
			seen[s.Body] = true
			scans = append(scans, scan{s.Body, s.Depth(), s.Type})
		}
	}
	for _, u := range b.units {
		if !seen[u.Site.Body] {
			seen[u.Site.Body] = true
			d := u.depth()
			scans = append(scans, scan{u.Site.Body, d, u.Site.Type})
		}
	}
	var out []Unread
	dup := map[token.Pos]bool{}
	for _, sc := range scans {
		var stack []ast.Node
		ast.Inspect(sc.body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			if lit, isLit := n.(*ast.FuncLit); isLit {
				if u, ok := b.unmappedClosure(lit, stack); ok {
					out = append(out, u)
				}
			}
			if ret, isRet := n.(*ast.ReturnStmt); isRet {
				if u, ok := b.returnedCall(ret, stack, sc.typ, sc.depth == 0 && b.directReturns); ok {
					out = append(out, u)
				}
			}
			call, ok := n.(*ast.CallExpr)
			if !ok || dup[call.Lparen] {
				return true
			}
			dup[call.Lparen] = true
			if b.wf.Role == HandlerValidator && sc.depth == 0 && isBuiltinPanic(b, call) {
				pos := b.fset.Position(call.Pos())
				out = append(out, Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: oneLine(types.ExprString(call)), Kind: UnreadValidatorPanic,
					Effect: "rejecting an update by panicking is not a path (a path that panics is left out), so the rejection is missing from this validator's map; reject by returning an error to have it mapped"})
			}
			var stmt ast.Node
			for i := len(stack) - 2; i >= 0; i-- {
				if _, isStmt := stack[i].(ast.Stmt); isStmt {
					stmt = stack[i]
					break
				}
			}
			if u, ok := b.unreadCall(call, followed[call], stmt, sc.depth); ok {
				out = append(out, u)
			}
			if u, ok := b.neverRead(call, stack, sc.body); ok {
				out = append(out, u)
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].File != out[k].File {
			return out[i].File < out[k].File
		}
		return out[i].Line < out[k].Line
	})
	return out
}

// takesContext: some argument of the call is a workflow.Context.
func (b *builder) takesContext(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		if t := b.info.TypeOf(a); t != nil && isWorkflowContext(t) {
			return true
		}
	}
	return false
}

// returnsError: the call's last result is an error.
func (b *builder) returnsError(call *ast.CallExpr) bool {
	t := b.info.TypeOf(call)
	if t == nil {
		return false
	}
	if tup, ok := t.(*types.Tuple); ok {
		return tup.Len() > 0 && types.Identical(tup.At(tup.Len()-1).Type(), errorType)
	}
	return types.Identical(t, errorType)
}

func (b *builder) unreadCall(call *ast.CallExpr, followed bool, stmt ast.Node, depth int) (Unread, bool) {
	if followed || !b.takesContext(call) {
		return Unread{}, false
	}
	if _, ok := b.wrapperFor(call); ok {
		return Unread{}, false // modelled as a Temporal call
	}
	// A conversion or a built-in is not a call of a function.
	if tv, ok := b.info.Types[call.Fun]; ok && (tv.IsType() || tv.IsBuiltin()) {
		return Unread{}, false
	}
	// A call that only builds a new context (workflow.WithValue and friends,
	// or a function of the project that sets options and returns the context)
	// takes no decision a path could differ by.
	if t := b.info.TypeOf(call); t != nil && isWorkflowContext(t) {
		return Unread{}, false
	}
	f := b.calledFunc(call)
	if isPkgFunc(f, workflowPkg, "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow") && len(call.Args) > 1 {
		tv, ok := b.info.Types[call.Args[1]]
		if basic, isBasic := types.Unalias(tv.Type).Underlying().(*types.Basic); ok && isBasic && basic.Info()&types.IsString != 0 && tv.Value == nil {
			pos := b.fset.Position(call.Pos())
			return Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: oneLine(types.ExprString(call)), Kind: UnreadByName,
				Effect: "one step named after the expression; which activity or workflow runs is decided at run time and is not read"}, true
		}
	}
	if f != nil && f.Pkg() != nil && strings.HasPrefix(f.Pkg().Path()+"/", sdkPrefix) {
		return Unread{}, false // the SDK: modelled, or deliberately not a step
	}
	pos := b.fset.Position(call.Pos())
	u := Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: oneLine(types.ExprString(call))}
	failsNote := ""
	if b.returnsError(call) {
		failsNote = "; an error check on its result is not a decision on the map (it is assumed to pass)"
	}
	switch {
	case f == nil:
		u.Kind, u.Effect = UnreadFuncValue, "not followed: PathKit cannot tell which function runs"+failsNote
	case isInterfaceMethod(f):
		u.Kind, u.Effect = UnreadInterfaceCall, "not followed: any type could be behind the interface"+failsNote
	case f.Pkg() == b.wf.Pkg.Types:
		h := b.helperDecls()[f]
		if h == nil || !b.worthInlining(h, 0) {
			return Unread{}, false // no workflow work in it: nothing to read
		}
		u.Kind, u.Effect = UnreadHelper, "not followed: "+b.whyNotFollowed(call, h, stmt, depth)+failsNote
	default:
		u.Kind = UnreadOtherPackage
		if m := b.moduleOf(f); m != "" && m != b.moduleOf(nil) {
			u.Kind = UnreadOtherModule
		}
		u.Effect = "not followed: only functions of the workflow's own package are read" + failsNote
	}
	if b.returnsError(call) && u.Kind != UnreadHelper {
		if name := wrapperName(f); name != "" {
			u.Suggestion = name
		}
	}
	return u, true
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

func isInterfaceMethod(f *types.Func) bool {
	recv := f.Type().(*types.Signature).Recv()
	return recv != nil && types.IsInterface(recv.Type())
}

// moduleOf is the module path a function belongs to ("" if unknown); nil
// stands for the workflow's own package.
func (b *builder) moduleOf(f *types.Func) string {
	pkg := b.wf.Pkg
	if f != nil && f.Pkg() != nil {
		if f.Pkg() != pkg.Types {
			if imp := pkg.Imports[f.Pkg().Path()]; imp != nil {
				pkg = imp
			} else {
				return ""
			}
		}
	}
	if pkg.Module == nil {
		return ""
	}
	return pkg.Module.Path
}

// wrapperName writes a function the way classification.activity_wrappers
// names it: "pkgpath.Func" or "(*pkgpath.Type).Method".
func wrapperName(f *types.Func) string {
	if f == nil || f.Pkg() == nil {
		return ""
	}
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil {
		return f.Pkg().Path() + "." + f.Name()
	}
	t := recv.Type()
	ptr := false
	if p, ok := t.(*types.Pointer); ok {
		t, ptr = p.Elem(), true
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return ""
	}
	name := f.Pkg().Path() + "." + named.Obj().Name()
	if ptr {
		return "(*" + name + ")." + f.Name()
	}
	return "(" + name + ")." + f.Name()
}

// whyNotFollowed says why a helper of the workflow's package that does
// workflow work was not followed at this call.
func (b *builder) whyNotFollowed(call *ast.CallExpr, h *helperFn, stmt ast.Node, depth int) string {
	switch {
	case call.Ellipsis != token.NoPos:
		return "the call spreads a slice (...)"
	case !b.helperClean(h):
		return "its code holds a construct PathKit cannot map (goto, Go's select, workflow.Selector or a type parameter)"
	case depth >= maxHelperDepth:
		return "it is called more than " + itoa(maxHelperDepth) + " helpers down"
	}
	if idx, _ := b.ctxParam(h.typ); idx < 0 {
		return "its workflow.Context parameter has no name"
	}
	if b.isWorkflowFunc(h) {
		return "it is a workflow of its own"
	}
	if stmt != nil && b.inlineAt(stmt) == nil {
		return "the call is part of a larger statement or expression (for example an if condition); only a call that is a whole statement, the whole right-hand side of an assignment, or a whole return is followed"
	}
	return "it calls itself, or is called from inside its own code"
}

// DisplayFile shortens an Unread's file to its base name for messages that
// need one.
func (u Unread) DisplayFile() string { return filepath.Base(u.File) }

// startsFuture reports whether a call starts something whose result is read
// later through a future: an activity, a child workflow, a timer, a Nexus
// operation.
func (b *builder) startsFuture(call *ast.CallExpr) bool {
	f := b.calledFunc(call)
	return isPkgFunc(f, workflowPkg, "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow", "NewTimer", "NewTimerWithOptions") || isNexusExecute(f)
}

// neverRead reports a call that starts a future nobody reads: the call is a
// statement of its own, is assigned to _, or is kept in a variable that is
// used nowhere (but in "_ = v"). stack is the path of nodes down to the call.
func (b *builder) neverRead(call *ast.CallExpr, stack []ast.Node, body *ast.BlockStmt) (Unread, bool) {
	if !b.startsFuture(call) || len(stack) < 2 {
		return Unread{}, false
	}
	var obj types.Object
	switch p := stack[len(stack)-2].(type) {
	case *ast.ExprStmt:
	case *ast.AssignStmt:
		if len(p.Lhs) != 1 || len(p.Rhs) != 1 {
			return Unread{}, false
		}
		id, ok := p.Lhs[0].(*ast.Ident)
		if !ok {
			return Unread{}, false
		}
		if id.Name != "_" {
			if obj = b.info.ObjectOf(id); obj == nil {
				return Unread{}, false
			}
		}
	case *ast.ValueSpec:
		if len(p.Names) != 1 || len(p.Values) != 1 {
			return Unread{}, false
		}
		if p.Names[0].Name != "_" {
			if obj = b.info.ObjectOf(p.Names[0]); obj == nil {
				return Unread{}, false
			}
		}
	default:
		return Unread{}, false
	}
	if obj != nil && b.usedAnywhere(obj, body) {
		return Unread{}, false
	}
	pos := b.fset.Position(call.Pos())
	label, _ := b.futureTargetLabel(call)
	return Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: oneLine(types.ExprString(call)), Kind: UnreadFutureNeverRead,
		Effect: "started, but no code reads its result" + label + ": it is not a step of any path, and its failure is not a decision"}, true
}

// futureTargetLabel names what a future-starting call starts, for messages.
func (b *builder) futureTargetLabel(call *ast.CallExpr) (string, bool) {
	l := b.futureLabel(call, call, 0)
	if l == "Future (future)" {
		return "", false
	}
	return " (" + l + ")", true
}

// usedAnywhere: the variable is used in body somewhere other than as "_ = v".
func (b *builder) usedAnywhere(obj types.Object, body *ast.BlockStmt) bool {
	blank := map[*ast.Ident]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			if l, ok := as.Lhs[0].(*ast.Ident); ok && l.Name == "_" {
				if r, ok := ast.Unparen(as.Rhs[0]).(*ast.Ident); ok {
					blank[r] = true
				}
			}
		}
		return true
	})
	used := false
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !blank[id] && b.info.Uses[id] == obj {
			used = true
		}
		return !used
	})
	return used
}

// returnedCall reports a "return <call>" of the shapes direct_returns is about
// that is not a decision on the map: in a helper, or anywhere when
// classification.direct_returns is "report". stack is the path of nodes down
// to the return; a return inside a function literal belongs to that literal.
func (b *builder) returnedCall(ret *ast.ReturnStmt, stack []ast.Node, typ *ast.FuncType, modelled bool) (Unread, bool) {
	if modelled || len(ret.Results) == 0 || len(ret.Results) != countResults(typ) || b.errResult(typ) < 0 {
		return Unread{}, false
	}
	for _, n := range stack {
		if _, lit := n.(*ast.FuncLit); lit {
			return Unread{}, false
		}
	}
	call, ok := ast.Unparen(ret.Results[len(ret.Results)-1]).(*ast.CallExpr)
	if !ok {
		return Unread{}, false
	}
	label, ok := b.directCallLabel(call)
	if !ok {
		return Unread{}, false
	}
	pos := b.fset.Position(ret.Pos())
	return Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: oneLine(types.ExprString(call)), Kind: UnreadReturnedCall,
		Effect: label + " is returned directly, so its failure and its success are one path (a test of either covers it); set classification.direct_returns to \"step\" to make them two paths"}, true
}

// unmappedClosure reports a function literal with Temporal calls inside that
// PathKit does not map: not the callback of a Selector (mapped with it), not
// an update, query or validator handler (a small workflow of its own), and not
// the deferred function of a compensation (a note on the path).
func (b *builder) unmappedClosure(lit *ast.FuncLit, stack []ast.Node) (Unread, bool) {
	if !b.containsTemporalCall(lit.Body) || len(stack) < 2 {
		return Unread{}, false
	}
	switch p := stack[len(stack)-2].(type) {
	case *ast.CallExpr:
		if b.isSelectorMethod(p) || isSelectorAdd(b, p) {
			return Unread{}, false
		}
		f := b.calledFunc(p)
		if isPkgFunc(f, workflowPkg, "SetUpdateHandler", "SetUpdateHandlerWithOptions", "SetQueryHandler") {
			return Unread{}, false
		}
		// the deferred call itself: defer func() { ... }()
		if len(stack) >= 3 {
			if d, ok := stack[len(stack)-3].(*ast.DeferStmt); ok && d.Call == p && b.isCompensation(d) {
				return Unread{}, false
			}
		}
	case *ast.KeyValueExpr: // UpdateHandlerOptions{Validator: func...}
		if id, ok := p.Key.(*ast.Ident); ok && id.Name == "Validator" {
			return Unread{}, false
		}
	}
	pos := b.fset.Position(lit.Pos())
	return Unread{Workflow: b.wf.Name, File: pos.Filename, Line: pos.Line, Call: "func literal", Kind: UnreadClosure,
		Effect: "its Temporal calls are not steps of any path (a goroutine, a deferred cleanup or a function stored for later is not followed)"}, true
}

// isSelectorAdd: a call of a method of a Selector, such as AddReceive.
func isSelectorAdd(b *builder, call *ast.CallExpr) bool {
	f := b.calledFunc(call)
	if f == nil {
		return false
	}
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == "Selector" && n.Obj().Pkg() != nil && strings.HasPrefix(n.Obj().Pkg().Path()+"/", sdkPrefix)
}

func isBuiltinPanic(b *builder, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	_, builtin := b.info.Uses[id].(*types.Builtin)
	return builtin
}

// depth is how many helpers deep the unit's code is: 1 for a helper called from
// the workflow function, one more for each helper it was reached through (by
// the first call that reached it).
func (u *HelperUnit) depth() int {
	d := 1
	for f := u; len(f.Calls) > 0 && f.Calls[0].From != nil; f = f.Calls[0].From {
		d++
	}
	return d
}
