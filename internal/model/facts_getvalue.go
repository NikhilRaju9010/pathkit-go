package model

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// extractGetValue handles values that Temporal fills in for the workflow:
//
//	var res *Result
//	err := workflow.ExecuteActivity(...).Get(ctx, &res)   // the one write
//	if res.Approved { ... }                               // reads
//	if res.Approved && other.Approved { ... }             // reads
//
// SSA cannot prove the two reads equal (the address of res leaves the
// function, into Get), so the variable is analysed here, with go/types.
// The variable is a "Get result" only when ALL of this holds:
//   - it is a local declared with no value, never used in a function
//     literal;
//   - its address is taken exactly once, as the second argument of a Get
//     method of the Temporal SDK, in a plain statement of the function (not
//     in defer or go, not in a loop or function literal);
//   - nothing else can write it: no assignment, ++/--, range target or &
//     on it or on anything inside it, no method call or method value on
//     it or its fields (a pointer-receiver method takes the address by
//     itself), and a pointer-typed variable is used only through field
//     reads and comparisons (passing the pointer on would let another
//     function change what it points to).
//
// A decision that reads one of its bool fields, as a plain chain of
// struct fields (res.Approved, res.Inner.Ok; through the variable's own
// pointer at most once), after that Get, reads a value nothing can change.
// Facts are attached per exit of the decision: the exit of `a && b` that
// is "true" tells a and b are both true; the exit of `a || b` that is
// "false" tells both are false; `!x` flips; anything else tells nothing.
func extractGetValue(b *builder, g *Graph) {
	var cands []*Junction
	for _, j := range g.Junctions {
		if _, ok := j.Stmt.(*ast.IfStmt); ok && j.Kind == PlainIf {
			cands = append(cands, j)
		}
	}
	if len(cands) == 0 {
		return
	}
	loops := b.insideLoop()

	uses := map[*types.Var][]useAt{}
	var stack []ast.Node
	hasGet := false
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
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Get" {
			hasGet = true
		}
		stack = append(stack, n)
		return true
	})
	if !hasGet {
		return
	}

	// The Get result variables, with the end position of their Get call.
	getEnd := map[*types.Var]token.Pos{}
	for v, us := range uses {
		if end, ok := b.getResultVar(v, us); ok {
			getEnd[v] = end
		}
	}
	if len(getEnd) == 0 {
		return
	}
	litSpans := b.funcLitSpans()

	for _, j := range cands {
		ifs := j.Stmt.(*ast.IfStmt)
		if loops[ifs] || inSpans(litSpans, ifs.Pos()) {
			continue
		}
		for _, exit := range []string{"true", "false"} {
			for _, a := range b.knownAtoms(ifs.Cond, exit == "true", getEnd, ifs.Pos()) {
				j.Facts = append(j.Facts, Fact{Kind: ValueKnown, Exit: exit, Key: a.key, Negated: !a.value,
					Detail: a.text + " is " + map[bool]string{true: "true", false: "false"}[a.value] + " (a value Temporal filled in once, with Get, and nothing changes afterwards)"})
			}
		}
	}
}

type atom struct {
	key   string
	text  string
	value bool
}

// knownAtoms lists the stable reads whose value is known when the
// condition came out as `result` (see extractGetValue).
func (b *builder) knownAtoms(cond ast.Expr, result bool, getEnd map[*types.Var]token.Pos, at token.Pos) []atom {
	cond = ast.Unparen(cond)
	switch c := cond.(type) {
	case *ast.UnaryExpr:
		if c.Op == token.NOT {
			return b.knownAtoms(c.X, !result, getEnd, at)
		}
	case *ast.BinaryExpr:
		switch {
		case c.Op == token.LAND && result, c.Op == token.LOR && !result:
			return append(b.knownAtoms(c.X, result, getEnd, at), b.knownAtoms(c.Y, result, getEnd, at)...)
		}
		return nil
	case *ast.SelectorExpr:
		if key, ok := b.stableFieldChain(c, getEnd, at); ok {
			return []atom{{key: key, text: types.ExprString(c), value: result}}
		}
	}
	return nil
}

// stableFieldChain: x.F1.F2... of bool type, rooted at a Get result
// variable, through value fields only (and the variable's own pointer once),
// read after the Get call.
func (b *builder) stableFieldChain(sel *ast.SelectorExpr, getEnd map[*types.Var]token.Pos, at token.Pos) (string, bool) {
	if t := b.info.TypeOf(sel); t == nil {
		return "", false
	} else if bt, ok := t.Underlying().(*types.Basic); !ok || bt.Info()&types.IsBoolean == 0 {
		return "", false
	}
	var names []string
	var e ast.Expr = sel
	depth := 0
	for {
		s, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			break
		}
		selection := b.info.Selections[s]
		if selection == nil || selection.Kind() != types.FieldVal || len(selection.Index()) != 1 {
			return "", false
		}
		names = append([]string{s.Sel.Name}, names...)
		e = s.X
		depth++
	}
	root, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return "", false
	}
	v, _ := b.info.Uses[root].(*types.Var)
	end, isGet := getEnd[v]
	if v == nil || !isGet || at < end {
		return "", false
	}
	// Indirections: a pointer variable is dereferenced once, by the first
	// selector; every later selector must be on a struct value.
	e = sel
	idx := depth
	for {
		s, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			break
		}
		idx--
		if b.info.Selections[s].Indirect() && !(idx == 0 && isPointer(v.Type())) {
			return "", false
		}
		e = s.X
	}
	return "get:" + itoa(int(v.Pos())) + ":" + strings.Join(names, "."), true
}

func isPointer(t types.Type) bool {
	_, ok := t.Underlying().(*types.Pointer)
	return ok
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

type span struct{ from, to token.Pos }

func (b *builder) funcLitSpans() []span {
	var out []span
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			out = append(out, span{lit.Pos(), lit.End()})
			return false
		}
		return true
	})
	return out
}

func inSpans(spans []span, p token.Pos) bool {
	for _, s := range spans {
		if s.from <= p && p < s.to {
			return true
		}
	}
	return false
}

// getResultVar decides whether v is a Get result variable (see
// extractGetValue) and returns the end of its Get call.
func (b *builder) getResultVar(v *types.Var, uses []useAt) (token.Pos, bool) {
	switch u := v.Type().Underlying().(type) {
	case *types.Struct:
	case *types.Pointer:
		if _, ok := u.Elem().Underlying().(*types.Struct); !ok {
			return 0, false
		}
	default:
		return 0, false
	}
	ptr := isPointer(v.Type())
	var end token.Pos
	gets, decls := 0, 0
	for _, u := range uses {
		for _, a := range u.stack {
			if _, ok := a.(*ast.FuncLit); ok {
				return 0, false
			}
		}
		if b.info.Defs[u.id] == v {
			vs, ok := u.stack[len(u.stack)-1].(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 0 {
				return 0, false
			}
			decls++
			continue
		}
		// the chain of parentheses and field selections above the use
		pi := len(u.stack) - 1
		var top ast.Node = u.id
		whole := true // the variable itself (through parentheses), not a field of it
		for pi >= 0 {
			switch p := u.stack[pi].(type) {
			case *ast.ParenExpr:
				top = p
				pi--
				continue
			case *ast.SelectorExpr:
				if p.X == top {
					sel := b.info.Selections[p]
					if sel == nil || sel.Kind() != types.FieldVal {
						return 0, false // a method call or method value
					}
					top = p
					whole = false
					pi--
					continue
				}
			}
			break
		}
		if pi < 0 {
			return 0, false
		}
		switch p := u.stack[pi].(type) {
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				// the one allowed &v: second argument of a Get call
				var call *ast.CallExpr
				if pi >= 1 {
					call, _ = u.stack[pi-1].(*ast.CallExpr)
				}
				if !whole || call == nil || len(call.Args) != 2 || ast.Unparen(call.Args[1]) != ast.Expr(p) || !b.isTemporalGet(call) {
					return 0, false
				}
				for _, a := range u.stack {
					switch a.(type) {
					case *ast.DeferStmt, *ast.GoStmt, *ast.ForStmt, *ast.RangeStmt:
						return 0, false
					}
				}
				gets++
				end = call.End()
			} else if p.Op != token.NOT {
				return 0, false
			}
		case *ast.AssignStmt:
			for _, l := range p.Lhs {
				if rootIs(l, u.id) {
					return 0, false
				}
			}
			if ptr && whole {
				return 0, false // the pointer is copied: aliasing
			}
		case *ast.IncDecStmt:
			return 0, false
		case *ast.RangeStmt:
			if rootIs(p.Key, u.id) || rootIs(p.Value, u.id) {
				return 0, false
			}
			if ptr && whole {
				return 0, false
			}
		case *ast.BinaryExpr:
		case *ast.IfStmt, *ast.ReturnStmt, *ast.ValueSpec, *ast.KeyValueExpr, *ast.CompositeLit:
			if ptr && whole {
				return 0, false
			}
		case *ast.CallExpr:
			isArg := false
			for _, arg := range p.Args {
				isArg = isArg || ast.Unparen(arg) == top
			}
			if !isArg || (ptr && whole) {
				return 0, false
			}
		case *ast.IndexExpr, *ast.SliceExpr:
			// v.Items[i] read; a write through it is caught as an
			// assignment target whose root is v
			if ptr && whole {
				return 0, false
			}
		default:
			return 0, false
		}
	}
	if gets != 1 || decls != 1 {
		return 0, false
	}
	return end, true
}

// rootIs: the expression is id, or a selector/index/star/slice chain whose
// root is id.
func rootIs(e ast.Expr, id *ast.Ident) bool {
	for e != nil {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			return x == id
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return false
		}
	}
	return false
}

// isTemporalGet: the call is a Get method of a type of the Temporal SDK
// (workflow.Future, ChildWorkflowFuture, ...).
func (b *builder) isTemporalGet(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Get" {
		return false
	}
	s := b.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false
	}
	fn, ok := s.Obj().(*types.Func)
	return ok && fn.Pkg() != nil && strings.HasPrefix(fn.Pkg().Path(), "go.temporal.io/sdk/")
}
