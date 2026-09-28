package model

import (
	"go/ast"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// Import paths of the Temporal SDK packages PathKit recognizes calls from.
// Aliases such as workflow.Future point at types in the SDK's internal
// package, so method checks look there.
const (
	workflowPkg = "go.temporal.io/sdk/workflow"
	internalPkg = "go.temporal.io/sdk/internal"
	temporalPkg = "go.temporal.io/sdk/temporal"
	sdkPrefix   = "go.temporal.io/sdk/"
)

var errorType = types.Universe.Lookup("error").Type()

// calledFunc returns the function or method a call invokes, or nil (for
// example for a call through a function variable).
func (b *builder) calledFunc(call *ast.CallExpr) *types.Func {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		f, _ := b.info.Uses[fun].(*types.Func)
		return f
	case *ast.SelectorExpr:
		f, _ := b.info.Uses[fun.Sel].(*types.Func)
		return f
	}
	return nil
}

// isPkgFunc reports whether f is a package-level function in pkgPath with
// one of the given names.
func isPkgFunc(f *types.Func, pkgPath string, names ...string) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != pkgPath {
		return false
	}
	if f.Type().(*types.Signature).Recv() != nil {
		return false
	}
	return len(names) == 0 || slices.Contains(names, f.Name())
}

// isSDKMethod reports whether f is a method named one of names, declared
// on one of the SDK's internal types (Future, Selector, ReceiveChannel, ...).
func isSDKMethod(f *types.Func, typeNames []string, names ...string) bool {
	if f == nil || !slices.Contains(names, f.Name()) {
		return false
	}
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	t := types.Unalias(recv.Type())
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == internalPkg && slices.Contains(typeNames, named.Obj().Name())
}

// isTemporalCall reports whether a call goes into any Temporal SDK package.
func (b *builder) isTemporalCall(call *ast.CallExpr) bool {
	f := b.calledFunc(call)
	return f != nil && f.Pkg() != nil && strings.HasPrefix(f.Pkg().Path(), sdkPrefix)
}

// errSource describes where an error variable's value came from, using its
// nearest assignment before pos. temporal is true when that is one of the
// Temporal calls whose error check is a junction (CLAUDE.md D2).
func (b *builder) errSource(obj types.Object, ifs *ast.IfStmt) (label string, temporal bool) {
	rhs, _, ok := b.nearestAssign(obj, ifs.Cond.Pos())
	if !ok {
		return "error (call)", false
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok {
		return "error (call)", false
	}
	f := b.calledFunc(call)
	switch {
	case isSDKMethod(f, []string{"Future"}, "Get"):
		sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		return b.futureLabel(sel.X, call, 0), true
	case isPkgFunc(f, workflowPkg, "Sleep"):
		return "Sleep (timer)", true
	case isPkgFunc(f, workflowPkg, "Await", "AwaitWithTimeout"):
		return f.Name() + " (wait)", true
	case f != nil:
		return f.Name() + " (call)", false
	}
	return "error (call)", false
}

// futureLabel names the Temporal call that produced a future: the activity
// or child workflow name for ExecuteActivity/ExecuteChildWorkflow, and so on.
func (b *builder) futureLabel(x ast.Expr, at ast.Node, depth int) string {
	call := b.originCall(x, at, depth)
	if call == nil {
		return "Future (future)"
	}
	f := b.calledFunc(call)
	switch {
	case isPkgFunc(f, workflowPkg, "ExecuteActivity") && len(call.Args) > 1:
		return argName(call.Args[1]) + " (activity)"
	case isPkgFunc(f, workflowPkg, "ExecuteLocalActivity") && len(call.Args) > 1:
		return argName(call.Args[1]) + " (local activity)"
	case isPkgFunc(f, workflowPkg, "ExecuteChildWorkflow") && len(call.Args) > 1:
		return argName(call.Args[1]) + " (child workflow)"
	case isPkgFunc(f, workflowPkg, "NewTimer", "NewTimerWithOptions"):
		return "NewTimer (timer)"
	case isPkgFunc(f, workflowPkg, "SignalExternalWorkflow"):
		return "SignalExternalWorkflow (signal)"
	case isPkgFunc(f, workflowPkg, "RequestCancelExternalWorkflow"):
		return "RequestCancelExternalWorkflow (cancel)"
	}
	return "Future (future)"
}

// originCall follows x back to the call that produced its value: x itself
// when it is a call, or, for a variable, the call in its nearest
// assignment before at (up to 3 variables deep). nil when there is none.
func (b *builder) originCall(x ast.Expr, at ast.Node, depth int) *ast.CallExpr {
	x = ast.Unparen(x)
	if id, ok := x.(*ast.Ident); ok && depth < 3 {
		if obj := b.info.Uses[id]; obj != nil {
			if rhs, _, ok := b.nearestAssign(obj, at.Pos()); ok {
				return b.originCall(rhs, at, depth+1)
			}
		}
	}
	call, _ := x.(*ast.CallExpr)
	return call
}

// argName prints an activity/workflow argument: a function name, or the
// text of a string literal.
func argName(e ast.Expr) string {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.BasicLit:
		if s, err := strconv.Unquote(e.Value); err == nil {
			return s
		}
	}
	return types.ExprString(e)
}
