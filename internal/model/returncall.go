package model

import (
	"go/ast"

	"golang.org/x/tools/go/cfg"
)

// returnCallJunction turns "return fut.Get(ctx, nil)", "return
// workflow.Sleep(ctx, d)" and "return workflow.Await(ctx, cond)" into a
// decision: the call fails or succeeds, and the function ends (or the helper
// returns) accordingly. Before this such a return was a path with an unknown
// end that hid the failure outcome: a test of either outcome covered it.
//
// It only reads the exact shape, with the call as the last result of a
// function whose last result is an error; anything else returns false and
// is walked as before.
func (b *builder) returnCallJunction(ret *ast.ReturnStmt, cont func(retKind) Target, _ *cfg.Block) (Target, bool) {
	if !b.directReturns || len(ret.Results) == 0 {
		return Target{}, false
	}
	call, ok := ast.Unparen(ret.Results[len(ret.Results)-1]).(*ast.CallExpr)
	if !ok {
		return Target{}, false
	}
	label, ok := b.directCallLabel(call)
	if !ok {
		return Target{}, false
	}
	// Only the workflow function's own returns: the return of a helper or of
	// a selector callback goes on to other code, and is read as before.
	if cont != nil || b.cur != nil {
		return Target{}, false
	}
	typ := b.wf.Func.Type
	if namedResults(typ) && hasDefer(b.wf.Func.Body) {
		return Target{}, false
	}
	idx := b.errResult(typ)
	if idx < 0 || countResults(typ) != len(ret.Results) {
		return Target{}, false
	}
	j := b.junctions[b.key(ret)]
	if j == nil {
		j = b.newJunction(ret, ErrCheck, label)
		// It did not exist before direct returns were read: the paths' older stable
		// ids (config entries written then) are looked up without it.
		j.viaHelper = true
		b.sawDirectReturn = true
		j.Exits = []*Exit{
			{Label: "failure", Junction: j, To: Target{End: EndFailed}},
			{Label: "success", Junction: j, To: Target{End: EndCompleted}},
		}
	}
	return Target{Junction: j}, true
}

// directCallLabel is the label of a call whose error is a decision when it is
// returned: reading a future, a timer or a wait. A call of a helper is not
// one (its error is read through its summary at an error check).
func (b *builder) directCallLabel(call *ast.CallExpr) (string, bool) {
	f := b.calledFunc(call)
	if w, ok := b.wrapperFor(call); ok {
		return w.step(f), true
	}
	switch {
	case isSDKMethod(f, []string{"Future", "NexusOperationFuture", "ChildWorkflowFuture"}, "Get"):
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		return b.futureLabel(sel.X, call, 0), true
	case isPkgFunc(f, workflowPkg, "Sleep"):
		return "Sleep (timer)", true
	case isPkgFunc(f, workflowPkg, "Await"):
		return "Await (wait)", true
	}
	return "", false
}
