package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// extractListBuiltFrom marks two range loops when the second walks a list
// that the first one builds, one append per trip, with nothing able to skip
// the append:
//
//	var futures []workflow.Future            // starts empty
//	for _, c := range configs {              // loop A
//	    f := workflow.ExecuteActivity(...)   // nothing here can leave the body
//	    futures = append(futures, f)         // the only write to futures
//	}
//	for _, f := range futures { ... }        // loop B
//
// Then A runs at least once exactly when futures is not empty, so B is
// skipped exactly when A is skipped. This is a different proof than
// SameList (two loops over one value): the lists are different values, tied
// by how one is built.
//
// Everything must hold, or no fact is attached:
//   - the list is a local slice variable that starts empty (no value,
//     nil, []T{} or make([]T, 0...)), is written exactly once after that,
//     by `list = append(list, ...)` with at least one element and no `...`,
//     and has its address taken nowhere and is not used inside a closure;
//   - that append is a statement of loop A's own body, and no statement
//     before it in the body can break, continue, return or jump
//     (so every trip that starts reaches the append);
//   - A walks a slice, array or string (it runs at least once when that is
//     not empty), and B walks the list variable and comes after A;
//   - neither A nor B is inside another loop or a closure (another trip or
//     another call could see another state).
func extractListBuiltFrom(b *builder, g *Graph) {
	byStmt := map[*ast.RangeStmt]*Junction{}
	for _, j := range g.Junctions {
		if rs, ok := j.Stmt.(*ast.RangeStmt); ok && j.Kind == Loop {
			byStmt[rs] = j
		}
	}
	if len(byStmt) < 2 {
		return
	}
	loops := b.insideLoop()
	closures := b.insideFuncLit()

	// Every use of every local identifier, with its parent chain.
	uses := map[*types.Var][]useAt{}
	var stack []ast.Node
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok {
			obj, _ := b.info.Uses[id].(*types.Var)
			if obj == nil {
				obj, _ = b.info.Defs[id].(*types.Var)
			}
			if obj != nil && !obj.IsField() {
				uses[obj] = append(uses[obj], useAt{id, append([]ast.Node(nil), stack...)})
			}
		}
		stack = append(stack, n)
		return true
	})

	for _, bj := range g.Junctions {
		rsB, ok := bj.Stmt.(*ast.RangeStmt)
		if !ok || bj.Kind != Loop || loops[rsB] || closures[rsB] {
			continue
		}
		id, ok := ast.Unparen(rsB.X).(*ast.Ident)
		if !ok {
			continue
		}
		obj, _ := b.info.Uses[id].(*types.Var)
		if obj == nil {
			continue
		}
		if _, isSlice := obj.Type().Underlying().(*types.Slice); !isSlice {
			continue
		}
		rsA, ok := b.builtByUses(obj, uses[obj], byStmt)
		if !ok || rsA.End() > rsB.Pos() || loops[rsA] || closures[rsA] || !b.listLike(rsA.X) {
			continue
		}
		aj := byStmt[rsA]
		key := fmt.Sprintf("built:%d", rsA.Pos())
		bj.Facts = append(bj.Facts, Fact{Kind: ListBuiltFrom, Key: key,
			Detail: "this loop walks a list that another loop builds with one append per trip, so it is empty exactly when that loop does not run"})
		if !hasFact(aj, ListBuiltFrom, key) {
			aj.Facts = append(aj.Facts, Fact{Kind: ListBuiltFrom, Key: key,
				Detail: "this loop appends one element per trip to a list that a later loop walks"})
		}
	}
}

func hasFact(j *Junction, k FactKind, key string) bool {
	for _, f := range j.Facts {
		if f.Kind == k && f.Key == key {
			return true
		}
	}
	return false
}

// insideFuncLit returns the range statements that sit in a function literal.
func (b *builder) insideFuncLit() map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			ast.Inspect(lit.Body, func(c ast.Node) bool {
				if rs, ok := c.(*ast.RangeStmt); ok {
					out[rs] = true
				}
				return true
			})
			return false
		}
		return true
	})
	return out
}

type useAt struct {
	id    *ast.Ident
	stack []ast.Node // ancestors, outermost first, not including id
}

// builtByUses decides whether obj is a list built, one append per trip, by
// one of the range loops in byStmt, and returns that loop. uses are all
// uses of obj. See extractListBuiltFrom for the conditions.
func (b *builder) builtByUses(obj *types.Var, uses []useAt, byStmt map[*ast.RangeStmt]*Junction) (*ast.RangeStmt, bool) {
	var appendLoop *ast.RangeStmt
	appends, decls := 0, 0
	for _, u := range uses {
		// The parent, looking through parentheses: (list) is the list.
		pi := len(u.stack) - 1
		for pi > 0 {
			if _, ok := u.stack[pi].(*ast.ParenExpr); !ok {
				break
			}
			pi--
		}
		parent := u.stack[pi]
		for _, a := range u.stack {
			if _, ok := a.(*ast.FuncLit); ok {
				return nil, false // used in a closure
			}
		}
		if b.info.Defs[u.id] == obj {
			if !b.startsEmpty(u.id, parent) {
				return nil, false
			}
			decls++
			continue
		}
		// Only these ways of using the list are known not to change it.
		// Anything else is refused, in particular: &list (also in
		// parentheses), a method call or method value (a pointer-receiver
		// method takes &list on its own), *list, a type assertion.
		switch p := parent.(type) {
		case *ast.AssignStmt:
			if isLHS(p, u.id) {
				// the only allowed write: list = append(list, x...)
				if p.Tok != token.ASSIGN || len(p.Lhs) != 1 || len(p.Rhs) != 1 || !b.isAppendOf(p.Rhs[0], obj) {
					return nil, false
				}
				blk, ok := stackParent(u.stack[:pi+1], 2).(*ast.BlockStmt)
				rs, ok2 := stackParent(u.stack[:pi+1], 3).(*ast.RangeStmt)
				if !ok || !ok2 || rs.Body != blk || byStmt[rs] == nil {
					return nil, false
				}
				if !oneTripReachesIt(blk, p) {
					return nil, false
				}
				appendLoop = rs
				appends++
			}
			// on the right-hand side: a copy of the header, harmless
		case *ast.RangeStmt:
			if p.Key == u.id || p.Value == u.id {
				return nil, false
			}
		case *ast.CallExpr:
			isArg := false
			for _, arg := range p.Args {
				isArg = isArg || ast.Unparen(arg) == ast.Expr(u.id)
			}
			if !isArg {
				return nil, false
			}
		case *ast.IndexExpr, *ast.SliceExpr, *ast.ValueSpec, *ast.ReturnStmt,
			*ast.CompositeLit, *ast.KeyValueExpr, *ast.BinaryExpr:
		default:
			return nil, false
		}
	}
	if decls != 1 || appends != 1 || appendLoop == nil {
		return nil, false
	}
	return appendLoop, true
}

// stackParent is the n-th ancestor from the end of the chain (1 = parent).
func stackParent(stack []ast.Node, n int) ast.Node {
	if len(stack) < n {
		return nil
	}
	return stack[len(stack)-n]
}

func isLHS(a *ast.AssignStmt, id *ast.Ident) bool {
	for _, l := range a.Lhs {
		if ast.Unparen(l) == ast.Expr(id) {
			return true
		}
	}
	return false
}

// isAppendOf: the expression is the builtin append(obj, one or more
// elements) without "...".
func (b *builder) isAppendOf(e ast.Expr, obj *types.Var) bool {
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(c.Args) < 2 || c.Ellipsis != token.NoPos {
		return false
	}
	fun, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if bi, ok := b.info.Uses[fun].(*types.Builtin); !ok || bi.Name() != "append" {
		return false
	}
	first, ok := ast.Unparen(c.Args[0]).(*ast.Ident)
	return ok && b.info.Uses[first] == obj
}

// startsEmpty: the declaration gives the list no elements.
func (b *builder) startsEmpty(id *ast.Ident, parent ast.Node) bool {
	empty := func(e ast.Expr) bool {
		switch e := ast.Unparen(e).(type) {
		case *ast.Ident:
			_, isNil := b.info.Uses[e].(*types.Nil)
			return isNil
		case *ast.CompositeLit:
			return len(e.Elts) == 0
		case *ast.CallExpr:
			fun, ok := ast.Unparen(e.Fun).(*ast.Ident)
			if !ok || len(e.Args) < 2 {
				return false
			}
			if bi, ok := b.info.Uses[fun].(*types.Builtin); !ok || bi.Name() != "make" {
				return false
			}
			tv, ok := b.info.Types[e.Args[1]]
			return ok && tv.Value != nil && tv.Value.String() == "0"
		}
		return false
	}
	switch p := parent.(type) {
	case *ast.ValueSpec:
		if len(p.Names) != 1 {
			return false
		}
		return len(p.Values) == 0 || (len(p.Values) == 1 && empty(p.Values[0]))
	case *ast.AssignStmt:
		return p.Tok == token.DEFINE && len(p.Lhs) == 1 && len(p.Rhs) == 1 && empty(p.Rhs[0])
	}
	return false
}

// oneTripReachesIt: no statement of the loop body before stmt can leave the
// body early (break, continue, goto, return, a labelled statement) or
// panic. Function literals are not looked into: their body runs elsewhere.
func oneTripReachesIt(body *ast.BlockStmt, stmt ast.Stmt) bool {
	for _, s := range body.List {
		if s == stmt {
			return true
		}
		ok := true
		ast.Inspect(s, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BranchStmt, *ast.ReturnStmt, *ast.LabeledStmt:
				ok = false
			case *ast.CallExpr:
				if id, isID := ast.Unparen(n.Fun).(*ast.Ident); isID && id.Name == "panic" {
					ok = false
				}
			}
			return ok
		})
		if !ok {
			return false
		}
	}
	return false
}
