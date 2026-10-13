package model

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// extractMore records, on every plain if that tests a "more" result, which
// exit means "the channel is closed" and where the channel came from.
//
// A "more" result comes from Receive (its only result), ReceiveWithTimeout
// or ReceiveAsyncWithMoreFlag (their second result), or the second
// parameter of an AddReceive callback. Only these shapes of condition are
// read: more, !more, more == false/true, more != false/true (either
// order). Anything compound (!more && x) records nothing: a missing fact
// is always the safe side.
func extractMore(b *builder, g *Graph) {
	for _, j := range g.Junctions {
		ifs, ok := j.Stmt.(*ast.IfStmt)
		if !ok || j.Kind != PlainIf {
			continue
		}
		id, closedExit, ok := b.moreCondition(ifs.Cond)
		if !ok {
			continue
		}
		obj, _ := b.info.Uses[id].(*types.Var)
		if obj == nil {
			continue
		}
		if basic, ok := obj.Type().Underlying().(*types.Basic); !ok || basic.Kind() != types.Bool {
			continue
		}
		chanExpr, known, found := b.moreSource(obj)
		if !found {
			continue
		}
		f := Fact{Kind: MoreCheck, Exit: closedExit, Origin: OriginUnknown}
		if known {
			f.Origin, f.Name = b.channelOrigin(chanExpr, 0)
			if f.Origin == OriginSignal {
				// Another handle of the same named channel can be turned
				// into a Channel and closed (see facts_signalclose.go).
				if t := b.signalCloseTaint(); t.all || (len(t.names) > 0 && (f.Name == "" || t.names[f.Name])) {
					f.Origin = OriginUnknown
				}
			}
		}
		f.Detail = "the 'more' result of a channel receive"
		j.Facts = append(j.Facts, f)
	}
}

// moreCondition reads the condition shapes listed above. It returns the
// identifier and the label of the exit taken when the value is false (the
// channel is closed).
func (b *builder) moreCondition(cond ast.Expr) (id *ast.Ident, closedExit string, ok bool) {
	cond = ast.Unparen(cond)
	switch c := cond.(type) {
	case *ast.Ident:
		if b.boolLit(c) == "" {
			return c, "false", true
		}
	case *ast.UnaryExpr:
		if x, isID := ast.Unparen(c.X).(*ast.Ident); isID && c.Op == token.NOT && b.boolLit(x) == "" {
			return x, "true", true
		}
	case *ast.BinaryExpr:
		if c.Op != token.EQL && c.Op != token.NEQ {
			return nil, "", false
		}
		x, y := ast.Unparen(c.X), ast.Unparen(c.Y)
		lit := ""
		var v *ast.Ident
		if xi, isID := x.(*ast.Ident); isID {
			if l := b.boolLit(xi); l == "" {
				v = xi
				if yi, isID := y.(*ast.Ident); isID {
					lit = b.boolLit(yi)
				}
			}
		}
		if v == nil {
			if yi, isID := y.(*ast.Ident); isID && b.boolLit(yi) == "" {
				v = yi
				if xi, isID := x.(*ast.Ident); isID {
					lit = b.boolLit(xi)
				}
			}
		}
		if v == nil || lit == "" {
			return nil, "", false
		}
		// "v == false" and "v != true" both mean "closed".
		closedWhenTrue := (c.Op == token.EQL) == (lit == "false")
		if closedWhenTrue {
			return v, "true", true
		}
		return v, "false", true
	}
	return nil, "", false
}

// boolLit returns "true" or "false" when id is the predeclared constant,
// else "".
func (b *builder) boolLit(id *ast.Ident) string {
	if b.info.Uses[id] == types.Universe.Lookup("true") {
		return "true"
	}
	if b.info.Uses[id] == types.Universe.Lookup("false") {
		return "false"
	}
	return ""
}

// recvKind says, for a call, whether it is a channel receive with a "more"
// result, which channel it reads, and which result is "more".
func (b *builder) recvKind(call *ast.CallExpr) (ch ast.Expr, moreIdx int, ok bool) {
	f := b.calledFunc(call)
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSel {
		return nil, 0, false
	}
	switch {
	case isSDKMethod(f, []string{"ReceiveChannel", "Channel"}, "Receive"):
		return sel.X, 0, true
	case isSDKMethod(f, []string{"ReceiveChannel", "Channel"}, "ReceiveWithTimeout", "ReceiveAsyncWithMoreFlag"):
		return sel.X, 1, true
	}
	return nil, 0, false
}

// moreSource finds where the variable v got its value as a "more" result.
// found is false when it is not a "more" result at all (an ordinary bool
// that happens to be named more). known is false when it is one but no
// single channel can be named (assigned several times, address taken).
func (b *builder) moreSource(v *types.Var) (ch ast.Expr, known, found bool) {
	// 1. The second parameter of an AddReceive callback.
	var cb ast.Expr
	hit := false
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !b.isSelectorMethod(call, "AddReceive") || len(call.Args) < 2 {
			return true
		}
		lit, ok := ast.Unparen(call.Args[1]).(*ast.FuncLit)
		if !ok {
			return true
		}
		var names []*ast.Ident
		for _, f := range lit.Type.Params.List {
			names = append(names, f.Names...)
		}
		if len(names) == 2 && b.info.Defs[names[1]] == v {
			cb, hit = call.Args[0], true
		}
		return true
	})
	writes, escaped := b.writesTo(v)
	if hit {
		return cb, len(writes) == 0 && !escaped, true
	}

	// 2. A variable set from Receive / ReceiveWithTimeout / ...WithMoreFlag.
	var first ast.Expr
	count := 0
	for _, w := range writes {
		call, ok := ast.Unparen(w.rhs).(*ast.CallExpr)
		if !ok {
			continue
		}
		if c, idx, ok := b.recvKind(call); ok && idx == w.index {
			count++
			first = c
		}
	}
	if count == 0 {
		return nil, false, false
	}
	// Known only for a single := or var x = ... : a named result or an
	// assignment later in the function may be read before it is written.
	return first, count == 1 && len(writes) == 1 && writes[0].define && !escaped, true
}

// write is one place a variable is given a value.
type write struct {
	rhs    ast.Expr // the value (the whole call for a multi-value call); nil when none (zero value, range)
	index  int      // which result of rhs this variable receives
	define bool     // := or var x = ...
}

// writesTo lists every assignment to obj in the workflow function,
// closures included. escaped is true when its address is taken (&x), after
// which any code could change it.
func (b *builder) writesTo(obj types.Object) (ws []write, escaped bool) {
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				id, ok := ast.Unparen(l).(*ast.Ident)
				if !ok || (b.info.Defs[id] != obj && b.info.Uses[id] != obj) {
					continue
				}
				w := write{define: n.Tok == token.DEFINE && b.info.Defs[id] == obj}
				switch {
				case len(n.Rhs) == len(n.Lhs):
					w.rhs = n.Rhs[i]
				case len(n.Rhs) == 1:
					w.rhs, w.index = n.Rhs[0], i
				}
				ws = append(ws, w)
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if b.info.Defs[name] != obj {
					continue
				}
				w := write{define: true}
				switch {
				case len(n.Values) == len(n.Names):
					w.rhs = n.Values[i]
				case len(n.Values) == 1:
					w.rhs, w.index = n.Values[0], i
				}
				ws = append(ws, w)
			}
		case *ast.IncDecStmt:
			if id, ok := ast.Unparen(n.X).(*ast.Ident); ok && (b.info.Defs[id] == obj || b.info.Uses[id] == obj) {
				ws = append(ws, write{})
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{n.Key, n.Value} {
				if id, ok := e.(*ast.Ident); ok && (b.info.Defs[id] == obj || b.info.Uses[id] == obj) {
					ws = append(ws, write{})
				}
			}
		case *ast.UnaryExpr:
			if id, ok := ast.Unparen(n.X).(*ast.Ident); ok && n.Op == token.AND && b.info.Uses[id] == obj {
				escaped = true
			}
		}
		return true
	})
	return ws, escaped
}

// channelOrigin follows a channel expression back to the call that made it.
// It follows only a local variable set exactly once, by := or var x = ...,
// and never gets the address taken; everything else (a parameter, a field,
// an element, a helper's result, a variable assigned again, a variable
// declared outside the closure it is used in) is unknown.
func (b *builder) channelOrigin(x ast.Expr, depth int) (ChannelOrigin, string) {
	x = ast.Unparen(x)
	switch x := x.(type) {
	case *ast.CallExpr:
		f := b.calledFunc(x)
		switch {
		case isPkgFunc(f, workflowPkg, "GetSignalChannel", "GetSignalChannelWithOptions"):
			name := ""
			if len(x.Args) >= 2 {
				if tv, ok := b.info.Types[x.Args[1]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
					name = constant.StringVal(tv.Value)
				}
			}
			return OriginSignal, name
		case isPkgFunc(f, workflowPkg, "NewChannel", "NewBufferedChannel", "NewNamedChannel", "NewNamedBufferedChannel"):
			return OriginOwn, ""
		}
	case *ast.Ident:
		if depth >= 3 {
			break
		}
		if rhs, ok := b.singleDefinition(x); ok && b.onlyReceivedFrom(x) {
			return b.channelOrigin(rhs, depth+1)
		}
		if rhs, assign, ok := b.lateSingleAssignment(x); ok && b.onlyReceivedFromExcept(x, assign) {
			return b.channelOrigin(rhs, depth+1)
		}
	}
	return OriginUnknown, ""
}

// declaredOutsideItsClosure reports whether obj is declared outside the
// innermost function literal that contains the use. A closure may run at
// another time than the code around it, so PathKit does not follow it.
func (b *builder) declaredOutsideItsClosure(obj types.Object, use ast.Node) bool {
	var inner *ast.FuncLit
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Pos() <= use.Pos() && use.End() <= lit.End() {
			inner = lit
		}
		return true
	})
	return inner != nil && !(inner.Pos() <= obj.Pos() && obj.Pos() < inner.End())
}

// onlyReceivedFrom reports whether the channel variable is used only to
// call methods on (ch.Receive(...)) or as the channel of AddReceive. A
// type assertion (ch.(workflow.Channel).Close()) or passing it on could
// close it, so then PathKit does not claim to know where it ends up.
func (b *builder) onlyReceivedFrom(use *ast.Ident) bool { return b.onlyReceivedFromExcept(use, nil) }

// onlyReceivedFromExcept is onlyReceivedFrom, except that the left-hand
// side of the assignment allowed (the one assignment of a variable declared
// first, see lateSingleAssignment) is not a problem.
func (b *builder) onlyReceivedFromExcept(use *ast.Ident, allowed *ast.AssignStmt) bool {
	obj := b.info.Uses[use]
	ok := true
	var stack []ast.Node
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		id, isID := n.(*ast.Ident)
		if !isID || b.info.Uses[id] != obj || len(stack) < 2 {
			return true
		}
		switch parent := stack[len(stack)-2].(type) {
		case *ast.AssignStmt:
			if parent != allowed || len(parent.Lhs) != 1 || parent.Lhs[0] != ast.Expr(id) {
				ok = false
			}
		case *ast.SelectorExpr:
			if parent.X != ast.Expr(id) {
				ok = false
			}
		case *ast.CallExpr:
			if !(b.isSelectorMethod(parent, "AddReceive") && len(parent.Args) > 0 && parent.Args[0] == ast.Expr(id)) {
				ok = false
			}
		default:
			ok = false
		}
		return true
	})
	return ok
}
