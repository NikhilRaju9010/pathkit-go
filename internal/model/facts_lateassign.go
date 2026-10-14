package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// lateSingleAssignment follows a use of a local variable back to its value
// for the shape
//
//	var ch workflow.ReceiveChannel
//	ch = workflow.GetSignalChannel(ctx, "name")
//
// (declared with no value, assigned once). It is narrower than
// singleDefinition on purpose. All of this must hold:
//   - the declaration has no value and one name, and is a statement of a
//     block;
//   - the one assignment is `ch = <expr>` (a single `=` with one value) and
//     is a statement of that same block, after the declaration;
//   - every other use of the variable is after the assignment ends, and not
//     in a function literal (a closure could run before the assignment);
//   - its address is taken nowhere (not even in parentheses), and it is not
//     written any other way (no ++, no range target, no other assignment).
//
// Anything else returns false and the caller treats the origin as unknown.
func (b *builder) lateSingleAssignment(use *ast.Ident) (ast.Expr, *ast.AssignStmt, bool) {
	obj, _ := b.info.Uses[use].(*types.Var)
	if obj == nil || obj.IsField() {
		return nil, nil, false
	}
	type at struct {
		id    *ast.Ident
		stack []ast.Node
	}
	var uses []at
	var stack []ast.Node
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok && (b.info.Uses[id] == obj || b.info.Defs[id] == obj) {
			uses = append(uses, at{id, append([]ast.Node(nil), stack...)})
		}
		stack = append(stack, n)
		return true
	})

	var declBlock *ast.BlockStmt
	var declStmt ast.Stmt
	var assign *ast.AssignStmt
	for _, u := range uses {
		for _, a := range u.stack {
			if _, ok := a.(*ast.FuncLit); ok {
				return nil, nil, false
			}
		}
		parent := u.stack[len(u.stack)-1]
		if b.info.Defs[u.id] == obj {
			vs, ok := parent.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 0 || declStmt != nil || len(u.stack) < 3 {
				return nil, nil, false
			}
			ds, ok1 := u.stack[len(u.stack)-3].(*ast.DeclStmt)
			blk, ok2 := u.stack[len(u.stack)-4].(*ast.BlockStmt)
			if !ok1 || !ok2 {
				return nil, nil, false
			}
			declStmt, declBlock = ds, blk
			continue
		}
		// parentheses do not hide a write or an address
		pi := len(u.stack) - 1
		for pi > 0 {
			if _, ok := u.stack[pi].(*ast.ParenExpr); !ok {
				break
			}
			pi--
		}
		switch p := u.stack[pi].(type) {
		case *ast.AssignStmt:
			isLHS := false
			for _, l := range p.Lhs {
				isLHS = isLHS || ast.Unparen(l) == ast.Expr(u.id)
			}
			if isLHS {
				if assign != nil || p.Tok != token.ASSIGN || len(p.Lhs) != 1 || len(p.Rhs) != 1 {
					return nil, nil, false
				}
				assign = p
			}
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				return nil, nil, false
			}
		case *ast.IncDecStmt:
			return nil, nil, false
		case *ast.RangeStmt:
			if p.Key == u.id || p.Value == u.id {
				return nil, nil, false
			}
		}
	}
	if declBlock == nil || assign == nil {
		return nil, nil, false
	}
	// the assignment is a statement of the declaration's block, after it
	seenDecl, found := false, false
	for _, s := range declBlock.List {
		if s == declStmt {
			seenDecl = true
		}
		if s == ast.Stmt(assign) {
			found = seenDecl
		}
	}
	if !found {
		return nil, nil, false
	}
	// every other use comes after the assignment
	for _, u := range uses {
		if b.info.Defs[u.id] == obj {
			continue
		}
		if u.id.Pos() < assign.End() && !(assign.Pos() <= u.id.Pos()) {
			return nil, nil, false
		}
		if assign.Pos() <= u.id.Pos() && u.id.Pos() < assign.End() && u.id != assign.Lhs[0] {
			return nil, nil, false // read inside the assignment itself
		}
	}
	return assign.Rhs[0], assign, true
}
