package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// findUnsupported returns the first construct (in source order) that
// PathKit can't map, with the reason.
func (b *builder) findUnsupported() *UnsupportedError {
	var found *UnsupportedError
	never := func(construct string, n ast.Node, reason string) {
		found = &UnsupportedError{Construct: construct, Pos: b.fset.Position(n.Pos()), Reason: reason}
	}
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // closures (signal receivers, selector callbacks) aren't mapped
		case *ast.SelectStmt:
			never("select statement", n, "Temporal workflows must use workflow.Selector instead of Go's select")
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				never("goto", n, "PathKit maps break, continue and return, but not goto")
			}
		case *ast.ExprStmt:
			if call, ok := ast.Unparen(n.X).(*ast.CallExpr); ok && b.isSelectorMethod(call, "Select") {
				info, u := b.checkSelector(n, call)
				if u != nil {
					found = u
				} else {
					b.selectAt[n] = info
				}
				return false
			}
		case *ast.CallExpr:
			if b.isSelectorMethod(n, "Select") { // not a statement of its own
				never("workflow.Selector", n, "Select must be a statement of its own")
			}
		}
		return found == nil
	})
	return found
}

// isCompensation reports a saga compensation defer: the deferred call
// starts an activity, local activity or child workflow, directly or
// inside a deferred function literal. Other defers (defer cancel(), a
// deferred log call) are not compensation. (CLAUDE.md D3: noted on the
// path, never a branch.)
func (b *builder) isCompensation(d *ast.DeferStmt) bool {
	hit := false
	ast.Inspect(d.Call, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isPkgFunc(b.calledFunc(call), workflowPkg, "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow") {
			hit = true
		}
		return !hit
	})
	return hit
}

// containsTemporalCall reports whether n (including closures inside it)
// calls into the Temporal SDK.
func (b *builder) containsTemporalCall(n ast.Node) bool {
	hit := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && b.isTemporalCall(call) {
			hit = true
		}
		return !hit
	})
	return hit
}

var channelTypes = []string{"ReceiveChannel", "Channel"}

// waitResult reports an if condition that is exactly the "did it arrive?"
// result of AwaitWithTimeout, ReceiveWithTimeout or ReceiveAsync: "ok" or
// "!ok" where ok is the first result of such a call (nearest assignment),
// or an inline "c.ReceiveAsync(&v)" / "!c.ReceiveAsync(&v)". It returns
// the call's name and whether the condition is true when the thing
// arrived. Any other use (say "ok && x > 0") is a plain if.
func (b *builder) waitResult(cond ast.Expr) (name string, arrivedIsTrue, ok bool) {
	e := ast.Unparen(cond)
	arrivedIsTrue = true
	if u, isNot := e.(*ast.UnaryExpr); isNot && u.Op == token.NOT {
		arrivedIsTrue = false
		e = ast.Unparen(u.X)
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		if f := b.calledFunc(x); isSDKMethod(f, channelTypes, "ReceiveAsync") {
			return f.Name(), arrivedIsTrue, true
		}
	case *ast.Ident:
		obj, isVar := b.info.Uses[x].(*types.Var)
		if !isVar {
			break
		}
		rhs, idx, found := b.nearestAssign(obj, cond.Pos())
		if !found || idx != 0 {
			break
		}
		if call, isCall := ast.Unparen(rhs).(*ast.CallExpr); isCall {
			f := b.calledFunc(call)
			if isPkgFunc(f, workflowPkg, "AwaitWithTimeout") ||
				isSDKMethod(f, channelTypes, "ReceiveWithTimeout", "ReceiveAsync", "ReceiveAsyncWithMoreFlag") {
				return f.Name(), arrivedIsTrue, true
			}
		}
	}
	return "", false, false
}
