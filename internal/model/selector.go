package model

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/cfg"
)

// selectorInfo is one workflow.Selector in the one shape PathKit maps
// (CLAUDE.md D3): created with NewSelector in this function, its Add…
// calls plain statements in the same block, each with an inline func
// literal, and exactly one Select call after them.
type selectorInfo struct {
	name string
	stmt *ast.ExprStmt   // the "sel.Select(ctx)" statement
	adds []*ast.CallExpr // the Add… calls, in source order
}

var selectorAddMethods = []string{"AddReceive", "AddFuture", "AddDefault", "AddSend"}

// callbackArg is the position of the callback among each Add… method's
// arguments.
var callbackArg = map[string]int{"AddReceive": 1, "AddFuture": 1, "AddDefault": 0, "AddSend": 2}

func methodName(call *ast.CallExpr) string {
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

// isSelectorMethod reports a call of one of the named workflow.Selector
// methods.
func (b *builder) isSelectorMethod(call *ast.CallExpr, names ...string) bool {
	return isSDKMethod(b.calledFunc(call), []string{"Selector"}, names...)
}

// checkSelector checks that the selector whose Select statement is stmt
// has the shape PathKit maps. If not, it says why, and the workflow is
// skipped: a selector PathKit can't see all of would give a wrong map.
func (b *builder) checkSelector(stmt *ast.ExprStmt, call *ast.CallExpr) (*selectorInfo, *UnsupportedError) {
	never := func(n ast.Node, format string, args ...any) *UnsupportedError {
		return &UnsupportedError{Construct: "workflow.Selector", Pos: b.fset.Position(n.Pos()), Reason: fmt.Sprintf(format, args...)}
	}
	recv, ok := ast.Unparen(call.Fun.(*ast.SelectorExpr).X).(*ast.Ident)
	var obj *types.Var
	if ok {
		obj, _ = b.info.Uses[recv].(*types.Var)
	}
	if obj == nil {
		return nil, never(call, "the selector must be kept in a variable created with workflow.NewSelector in this function")
	}
	name := obj.Name()

	type addStmt struct {
		stmt   *ast.ExprStmt
		parent ast.Node // the block (or case) the statement sits in
		calls  []*ast.CallExpr
		inLit  bool   // inside a function literal
		inside string // the innermost if, loop, switch, select or function literal around it
	}
	var (
		created   ast.Stmt
		container ast.Node
		addStmts  []addStmt
		selects   int
		problem   *UnsupportedError
	)
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		use, isIdent := n.(*ast.Ident)
		if problem != nil || !isIdent || (b.info.Uses[use] != obj && b.info.Defs[use] != obj) {
			return problem == nil
		}
		path, _ := astutil.PathEnclosingInterval(b.wf.File, use.Pos(), use.End())
		inLit := slices.ContainsFunc(path, func(n ast.Node) bool { _, ok := n.(*ast.FuncLit); return ok })

		// Being assigned: the one place the selector is created.
		if value, assigned := assignedValue(path); assigned {
			c, isCall := ast.Unparen(value).(*ast.CallExpr)
			switch {
			case created != nil:
				problem = never(use, "the selector %s must be created exactly once, with workflow.NewSelector or NewNamedSelector", name)
				return false
			case !isCall || !isPkgFunc(b.calledFunc(c), workflowPkg, "NewSelector", "NewNamedSelector"):
				// e.g. a helper that makes it: PathKit can't see what that adds
				problem = never(use, "the selector %s must be created in this function, with workflow.NewSelector or NewNamedSelector", name)
				return false
			}
			created = path[1].(ast.Stmt)
			if _, isSpec := path[1].(*ast.ValueSpec); isSpec {
				created = path[3].(ast.Stmt) // ValueSpec -> GenDecl -> DeclStmt
			}
			container = parentOf(b.wf.File, created)
			return true
		}

		// Used as "name.Method(...)": a chain of Add… calls, Select, or
		// HasPending. Anything else hands the selector to other code.
		sel, isSel := path[1].(*ast.SelectorExpr)
		var mcall *ast.CallExpr
		if isSel && len(path) > 2 {
			mcall, _ = path[2].(*ast.CallExpr)
		}
		if mcall == nil || mcall.Fun != sel {
			problem = never(use, "the selector %s is also used here (passed on, stored or captured); PathKit can't see what is added to it there", name)
			return false
		}
		switch {
		case b.isSelectorMethod(mcall, "Select"):
			selects++
		case b.isSelectorMethod(mcall, "HasPending"):
		case b.isSelectorMethod(mcall, selectorAddMethods...):
			calls := []*ast.CallExpr{mcall}
			k := 2
			for k+2 < len(path) {
				next, ok1 := path[k+1].(*ast.SelectorExpr)
				ncall, ok2 := path[k+2].(*ast.CallExpr)
				if !ok1 || !ok2 || next.X != path[k] || ncall.Fun != next || !b.isSelectorMethod(ncall, selectorAddMethods...) {
					break
				}
				calls = append(calls, ncall)
				k += 2
			}
			es, isStmt := path[k+1].(*ast.ExprStmt)
			if !isStmt || es.X != path[k] {
				problem = never(calls[len(calls)-1], "%s must be a statement of its own (not chained into Select or used in an expression)", methodName(calls[len(calls)-1]))
				return false
			}
			addStmts = append(addStmts, addStmt{stmt: es, parent: path[k+2], calls: calls, inLit: inLit, inside: innermostBlockKind(path[k+2:])})
		default:
			problem = never(use, "the selector %s is used with a method PathKit doesn't know", name)
			return false
		}
		return true
	})
	if problem != nil {
		return nil, problem
	}
	if created == nil {
		return nil, never(call, "the selector %s must be created in this function, with workflow.NewSelector or NewNamedSelector", name)
	}
	if selects != 1 {
		return nil, never(call, "the selector %s has %d Select calls; PathKit maps exactly one", name, selects)
	}
	info := &selectorInfo{name: name, stmt: stmt}
	for _, a := range addStmts {
		first := a.calls[0]
		switch {
		case a.inLit || a.parent != container:
			return nil, never(first, "%s is inside %s; PathKit needs every Add… call in the same block as NewSelector, so every Select has the same exits", methodName(first), a.inside)
		case a.stmt.Pos() < created.Pos():
			return nil, never(first, "%s comes before the selector is created", methodName(first))
		case a.stmt.Pos() > stmt.Pos():
			return nil, never(first, "%s comes after the Select call", methodName(first))
		}
		for _, c := range a.calls {
			i := callbackArg[methodName(c)]
			if i >= len(c.Args) {
				return nil, never(c, "%s has no callback", methodName(c))
			}
			if _, isLit := ast.Unparen(c.Args[i]).(*ast.FuncLit); !isLit {
				return nil, never(c, "the callback of %s must be an inline func literal, so PathKit can follow it", methodName(c))
			}
			info.adds = append(info.adds, c)
		}
	}
	if len(info.adds) == 0 {
		return nil, never(call, "the selector %s has no Add… calls", name)
	}
	slices.SortFunc(info.adds, func(x, y *ast.CallExpr) int { return int(x.Pos() - y.Pos()) })
	return info, nil
}

// innermostBlockKind names the innermost construct in path (innermost
// first) that puts a statement in its own block: "an if", "a loop", "a
// switch", "a select" or "a function literal"; "a nested block" if none.
func innermostBlockKind(path []ast.Node) string {
	for _, n := range path {
		switch n.(type) {
		case *ast.FuncLit:
			return "a function literal"
		case *ast.IfStmt:
			return "an if"
		case *ast.ForStmt, *ast.RangeStmt:
			return "a loop"
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			return "a switch"
		case *ast.SelectStmt:
			return "a select"
		case *ast.FuncDecl:
			return "a nested block"
		}
	}
	return "a nested block"
}

// assignedValue reports whether the identifier path[0] is being assigned
// (":=", "=", or "var x = ..."), and the value it gets.
func assignedValue(path []ast.Node) (ast.Expr, bool) {
	id := path[0]
	switch p := path[1].(type) {
	case *ast.AssignStmt:
		for i, l := range p.Lhs {
			if l == id {
				if len(p.Rhs) == len(p.Lhs) {
					return p.Rhs[i], true
				}
				return nil, true // "a, b := f()": not a NewSelector call
			}
		}
	case *ast.ValueSpec:
		for i, n := range p.Names {
			if n == id {
				if i < len(p.Values) {
					return p.Values[i], true
				}
				return nil, true // "var sel workflow.Selector"
			}
		}
	}
	return nil, false
}

// parentOf returns the node directly enclosing n.
func parentOf(file *ast.File, n ast.Node) ast.Node {
	path, _ := astutil.PathEnclosingInterval(file, n.Pos(), n.End())
	for i, p := range path {
		if p == n && i+1 < len(path) {
			return path[i+1]
		}
	}
	return nil
}

// selectorJunction builds the junction at a Select call: one exit per
// Add… call. Each exit's road is its callback's body (walked with its own
// go/cfg graph); a return in the callback, or its end, goes on to the
// code after the Select call (node i+1 of blk).
func (b *builder) selectorJunction(info *selectorInfo, blk *cfg.Block, i int) Target {
	if j, ok := b.junctions[info.stmt]; ok {
		return Target{Junction: j}
	}
	j := b.newJunction(info.stmt, Selector, "select (Selector)")
	j.order = info.adds[0].Pos() // numbered where its exits are set up
	after := func() Target { return b.roadFrom(blk, i+1) }
	for _, add := range info.adds {
		lit := ast.Unparen(add.Args[callbackArg[methodName(add)]]).(*ast.FuncLit)
		e := addExit(j, b.selectorExitLabel(add), lit.Body)
		c := cfg.New(lit.Body, b.mayReturn)
		b.indexLoops(c)
		for _, cb := range c.Blocks {
			b.contOf[cb] = after
		}
		e.To = b.road(c.Blocks[0])
	}
	return Target{Junction: j}
}

// selectorExitLabel names what an Add… call waits for:
//
//	signal "name"      AddReceive on workflow.GetSignalChannel(ctx, "name")
//	timeout            AddFuture on a workflow.NewTimer future
//	activity Name      AddFuture on an ExecuteActivity future
//	local activity N   ... ExecuteLocalActivity
//	child Name         ... ExecuteChildWorkflow
//	default            AddDefault
//	receive x / send x / future x   anything else, as written
func (b *builder) selectorExitLabel(add *ast.CallExpr) string {
	arg0 := func() ast.Expr { return add.Args[0] }
	switch methodName(add) {
	case "AddDefault":
		return "default"
	case "AddSend":
		return "send " + types.ExprString(arg0())
	case "AddReceive":
		if c := b.originCall(arg0(), add, 0); c != nil && len(c.Args) > 1 &&
			isPkgFunc(b.calledFunc(c), workflowPkg, "GetSignalChannel", "GetSignalChannelWithOptions") {
			return "signal " + b.stringText(c.Args[1])
		}
		return "receive " + types.ExprString(arg0())
	case "AddFuture":
		if c := b.originCall(arg0(), add, 0); c != nil {
			f := b.calledFunc(c)
			switch {
			case isPkgFunc(f, workflowPkg, "NewTimer", "NewTimerWithOptions"):
				return "timeout"
			case isPkgFunc(f, workflowPkg, "ExecuteActivity") && len(c.Args) > 1:
				return "activity " + argName(c.Args[1])
			case isPkgFunc(f, workflowPkg, "ExecuteLocalActivity") && len(c.Args) > 1:
				return "local activity " + argName(c.Args[1])
			case isPkgFunc(f, workflowPkg, "ExecuteChildWorkflow") && len(c.Args) > 1:
				return "child " + argName(c.Args[1])
			}
		}
		return "future " + types.ExprString(arg0())
	}
	return methodName(add)
}

// stringText prints a string expression in quotes when its value is known
// ("delivery-update", also through a constant), else as written.
func (b *builder) stringText(e ast.Expr) string {
	if tv, ok := b.info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return strconv.Quote(constant.StringVal(tv.Value))
	}
	return types.ExprString(e)
}
