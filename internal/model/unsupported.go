package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// findUnsupported returns the first construct (in source order) that
// PathKit can't map: either never (never is set), or not until a later
// M4 slice, which adds it together with its recording.
func (b *builder) findUnsupported() *UnsupportedError {
	var found *UnsupportedError
	report := func(construct string, n ast.Node) {
		found = &UnsupportedError{Construct: construct, Pos: b.fset.Position(n.Pos())}
	}
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
		case *ast.ForStmt:
			report("for loop", n)
		case *ast.RangeStmt:
			report("range loop", n)
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				never("goto", n, "PathKit maps break, continue and return, but not goto")
			} else if n.Label != nil {
				report("labeled "+n.Tok.String(), n)
			}
		case *ast.LabeledStmt:
			report("label", n)
		case *ast.DeferStmt:
			if b.containsTemporalCall(n.Call) {
				report("defer with a Temporal call (saga compensation)", n)
			}
			return false
		case *ast.CallExpr:
			if isSDKMethod(b.calledFunc(n), []string{"Selector"}, "Select") {
				report("workflow.Selector", n)
			}
		case *ast.IfStmt:
			if name := b.waitResultIn(n); name != "" {
				report("result of "+name+" used in an if", n)
			}
		}
		return found == nil
	})
	return found
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

var receiveMethods = []string{"ReceiveWithTimeout", "ReceiveAsync", "ReceiveAsyncWithMoreFlag"}
var channelTypes = []string{"ReceiveChannel", "Channel"}

// waitResultIn returns the name of the call whose "did it arrive / did it
// time out" result the if's condition uses (AwaitWithTimeout's ok,
// ReceiveWithTimeout's ok, ReceiveAsync's result), or "".
func (b *builder) waitResultIn(ifs *ast.IfStmt) string {
	name := ""
	ast.Inspect(ifs.Cond, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if f := b.calledFunc(n); isSDKMethod(f, channelTypes, receiveMethods...) {
				name = f.Name()
			}
		case *ast.Ident:
			obj, ok := b.info.Uses[n].(*types.Var)
			if !ok {
				break
			}
			rhs, idx, found := b.nearestAssign(obj, ifs.Cond.Pos())
			if !found || idx != 0 {
				break
			}
			if call, ok := ast.Unparen(rhs).(*ast.CallExpr); ok {
				f := b.calledFunc(call)
				if isPkgFunc(f, workflowPkg, "AwaitWithTimeout") || isSDKMethod(f, channelTypes, receiveMethods...) {
					name = f.Name()
				}
			}
		}
		return name == ""
	})
	return name
}
