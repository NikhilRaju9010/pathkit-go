package model

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// extractLoopFirstTrip marks a `for cond { ... }` loop (no init, no post)
// whose condition is `v` or `!v` on a local bool variable with a known
// value when the loop starts, and the condition is true then:
//
//	done := false
//	for !done { ... }      // the first test is always true
//
// So the loop always runs at least once: a path that leaves it by `exit`
// without ever taking `iterate` is impossible.
//
// The value at the first test is known when:
//   - v is declared in the function (not a parameter, a field, or a
//     closure's variable) with a constant bool value, or no value (false),
//     before the loop, and the loop is in the declaration's scope;
//   - between the declaration and the loop, v is not written, not
//     address-taken, and not used in a function literal (a closure defined
//     there may run before the loop starts); uses inside the loop and after
//     it do not matter for the first test;
//   - every loop around the loop statement also contains the declaration,
//     so each trip of such a loop declares v again.
func extractLoopFirstTrip(b *builder, g *Graph) {
	loops := map[*ast.ForStmt]*Junction{}
	for _, j := range g.Junctions {
		if fs, ok := j.Stmt.(*ast.ForStmt); ok && j.Kind == Loop && fs.Init == nil && fs.Post == nil && fs.Cond != nil {
			loops[fs] = j
		}
	}
	if len(loops) == 0 {
		return
	}

	// Every use of every local variable, and the ancestors of every such
	// for statement.
	uses := map[*types.Var][]useAt{}
	enclosing := map[*ast.ForStmt][]ast.Node{}
	var stack []ast.Node
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n := n.(type) {
		case *ast.Ident:
			obj, _ := b.info.Uses[n].(*types.Var)
			if obj == nil {
				obj, _ = b.info.Defs[n].(*types.Var)
			}
			if obj != nil && !obj.IsField() {
				uses[obj] = append(uses[obj], useAt{n, append([]ast.Node(nil), stack...)})
			}
		case *ast.ForStmt:
			if loops[n] != nil {
				enclosing[n] = append([]ast.Node(nil), stack...)
			}
		}
		stack = append(stack, n)
		return true
	})

	for fs, j := range loops {
		skip := false
		for _, a := range enclosing[fs] {
			if _, ok := a.(*ast.FuncLit); ok {
				skip = true // another call or time may see another state
			}
		}
		if skip {
			continue
		}
		cond, negated := ast.Unparen(fs.Cond), false
		if u, ok := cond.(*ast.UnaryExpr); ok && u.Op == token.NOT {
			cond, negated = ast.Unparen(u.X), true
		}
		id, ok := cond.(*ast.Ident)
		if !ok {
			continue
		}
		obj, _ := b.info.Uses[id].(*types.Var)
		if obj == nil {
			continue
		}
		// plain bool only: a named bool type can have pointer-receiver methods
		if bt, ok := obj.Type().(*types.Basic); !ok || bt.Info()&types.IsBoolean == 0 {
			continue
		}
		value, ok := b.valueBeforeLoop(obj, uses[obj], fs, enclosing[fs])
		if !ok || value == negated { // the first test must be true
			continue
		}
		j.Facts = append(j.Facts, Fact{Kind: LoopFirstTripAlways, Exit: "exit",
			Detail: "the loop condition is true the first time it is tested (" + obj.Name() + " starts " + map[bool]string{true: "true", false: "false"}[value] + " and is not changed before the loop), so the loop runs at least once"})
	}
}

// valueBeforeLoop returns the constant bool value obj has when loop fs
// starts, when it is certain (see extractLoopFirstTrip).
func (b *builder) valueBeforeLoop(obj *types.Var, uses []useAt, fs *ast.ForStmt, around []ast.Node) (value, ok bool) {
	var declPos token.Pos
	declared := 0
	for _, u := range uses {
		if b.info.Defs[u.id] != obj {
			continue
		}
		parent := u.stack[len(u.stack)-1]
		declared++
		declPos = parent.End()
		switch p := parent.(type) {
		case *ast.ValueSpec:
			if len(p.Names) != 1 || len(p.Values) > 1 {
				return false, false
			}
			if len(p.Values) == 0 {
				value = false
			} else if v, k := b.constBool(p.Values[0]); k {
				value = v
			} else {
				return false, false
			}
		case *ast.AssignStmt:
			if p.Tok != token.DEFINE || len(p.Lhs) != 1 || len(p.Rhs) != 1 {
				return false, false
			}
			v, k := b.constBool(p.Rhs[0])
			if !k {
				return false, false
			}
			value = v
		default:
			return false, false // a parameter, a range variable, ...
		}
		for _, a := range u.stack {
			if _, isLit := a.(*ast.FuncLit); isLit {
				return false, false
			}
		}
		// every loop around the loop statement must also contain the
		// declaration: then v is declared again on each of its trips.
		for _, a := range around {
			switch a.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				if !(a.Pos() <= u.id.Pos() && u.id.Pos() < a.End()) {
					return false, false
				}
			}
		}
	}
	if declared != 1 || declPos >= fs.Pos() {
		return false, false
	}
	// Between the declaration and the loop: no write, no address, no use
	// in a function literal.
	for _, u := range uses {
		if b.info.Defs[u.id] == obj || u.id.Pos() >= fs.Pos() || u.id.Pos() < declPos {
			continue
		}
		// The parent, looking through parentheses: (v) is v.
		pi := len(u.stack) - 1
		for pi > 0 {
			if _, ok := u.stack[pi].(*ast.ParenExpr); !ok {
				break
			}
			pi--
		}
		for _, a := range u.stack {
			if _, isLit := a.(*ast.FuncLit); isLit {
				return false, false
			}
		}
		// Only plain reads are allowed here. Anything else is refused: a
		// write in any form, &v, a method call or method value (a
		// pointer-receiver method takes &v on its own), a range target.
		switch p := u.stack[pi].(type) {
		case *ast.AssignStmt:
			for _, l := range p.Lhs {
				if ast.Unparen(l) == ast.Expr(u.id) {
					return false, false
				}
			}
		case *ast.UnaryExpr:
			if p.Op != token.NOT {
				return false, false
			}
		case *ast.CallExpr:
			isArg := false
			for _, arg := range p.Args {
				isArg = isArg || ast.Unparen(arg) == ast.Expr(u.id)
			}
			if !isArg {
				return false, false
			}
		case *ast.BinaryExpr, *ast.IfStmt, *ast.ForStmt, *ast.ReturnStmt,
			*ast.ValueSpec, *ast.KeyValueExpr, *ast.CompositeLit:
		default:
			return false, false
		}
	}
	return value, true
}

// constBool: the expression is a constant bool (true, false, !true, a
// constant name).
func (b *builder) constBool(e ast.Expr) (value, ok bool) {
	tv, found := b.info.Types[e]
	if !found || tv.Value == nil || tv.Value.Kind() != constant.Bool {
		return false, false
	}
	return constant.BoolVal(tv.Value), true
}
