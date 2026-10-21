package scaffold

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// Inputs filled in from the facts on the path (milestone M11, L2). For the
// outcome a test must reach, every decision on the path says something about a
// value: equal to a constant, not equal to some constants, in a range, nil or
// not, empty or not, a bool, or an interface holding a type. The values are
// the workflow's input (a parameter or a field of it), what an activity gives
// back (so a later decision on a field of the result goes the right way) and
// what a signal carries. When a value cannot be derived, a TODO names the exact
// condition and the value it needs.

// root is a value the test can set: a parameter of the workflow, the result of
// an activity, or the payload of a signal.
type root struct {
	obj  types.Object // the variable in the workflow's code
	name string       // the variable in the test
	typ  types.Type
	kind string // "input", "result" or "payload"
}

// cons collects what is known about one target (a root or a field below it).
type cons struct {
	lhs, rootName string
	typ           types.Type
	eq            []constant.Value
	ne            []constant.Value
	lo, hi        *int64 // inclusive bounds
	nilWant       *bool  // true: must be nil, false: must not be
	minLen        *int64
	maxLen        *int64
	boolWant      *bool
	dyn           types.Type // the type an interface must hold
	dynNil        bool       // the interface must be nil
	conflict      string
	conds         []string // the conditions that asked, for the TODO
}

// solved is everything solveAll worked out.
type solved struct {
	inputs   []assign
	results  map[types.Object][]assign // by the variable that receives an activity's result
	payloads map[types.Object][]assign // by the variable a signal is received into
	roots    map[types.Object]*root
	unsolved []string
}

func i64(v int64) *int64 { return &v }
func bptr(v bool) *bool  { return &v }

// atom is one fact about one target.
type atom struct {
	e    ast.Expr
	op   token.Token // EQL NEQ LSS LEQ GTR GEQ
	val  constant.Value
	kind string // "cmp", "bool", "nil", "len", "dyn"
	nil_ bool   // for kind "nil": true for == nil
}

// roots finds the variables the test can set, in the workflow function.
func (b *builder) rootsOf(fn *ast.FuncDecl, params map[types.Object]string) map[types.Object]*root {
	rs := map[types.Object]*root{}
	for o, name := range params {
		rs[o] = &root{obj: o, name: name, typ: o.Type(), kind: "input"}
	}
	// activity results: x.Get(ctx, &v) where x is (or holds) an ExecuteActivity call
	futures := map[types.Object]*ast.CallExpr{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) == len(s.Rhs) {
				for i, r := range s.Rhs {
					if c, ok := ast.Unparen(r).(*ast.CallExpr); ok && b.isExecute(c) {
						if id, ok := s.Lhs[i].(*ast.Ident); ok {
							if o := b.info.ObjectOf(id); o != nil {
								futures[o] = c
							}
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, v := range s.Values {
				if c, ok := ast.Unparen(v).(*ast.CallExpr); ok && b.isExecute(c) && i < len(s.Names) {
					if o := b.info.ObjectOf(s.Names[i]); o != nil {
						futures[o] = c
					}
				}
			}
		}
		return true
	})
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) < 2 {
			return true
		}
		sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Get" {
			return true
		}
		u, ok := ast.Unparen(c.Args[len(c.Args)-1]).(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		id, ok := ast.Unparen(u.X).(*ast.Ident)
		if !ok {
			return true
		}
		v := b.info.ObjectOf(id)
		if v == nil {
			return true
		}
		var exec *ast.CallExpr
		switch recv := ast.Unparen(sel.X).(type) {
		case *ast.CallExpr:
			if b.isExecute(recv) {
				exec = recv
			}
		case *ast.Ident:
			exec = futures[b.info.ObjectOf(recv)]
		}
		if exec != nil {
			rs[v] = &root{obj: v, name: id.Name + "Out", typ: v.Type(), kind: "result"}
			b.execOf[v] = exec
		}
		return true
	})
	for _, obj := range b.signalPayloads(fn) {
		rs[obj] = &root{obj: obj, name: obj.Name() + "Payload", typ: obj.Type(), kind: "payload"}
	}
	return rs
}

func (b *builder) isExecute(c *ast.CallExpr) bool {
	var f *types.Func
	switch fun := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		f, _ = b.info.Uses[fun].(*types.Func)
	case *ast.SelectorExpr:
		f, _ = b.info.Uses[fun.Sel].(*types.Func)
	}
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != workflowPkg {
		return false
	}
	switch f.Name() {
	case "ExecuteActivity", "ExecuteLocalActivity", "ExecuteChildWorkflow":
		return true
	}
	return false
}

// signalPayloads maps a signal name to the variable its callback receives it
// into: ch := workflow.GetSignalChannel(ctx, "name"); sel.AddReceive(ch, func(c, more) { c.Receive(ctx, &v) }).
func (b *builder) signalPayloads(fn *ast.FuncDecl) map[string]types.Object {
	chans := map[types.Object]string{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, r := range as.Rhs {
			c, ok := ast.Unparen(r).(*ast.CallExpr)
			if !ok || len(c.Args) != 2 {
				continue
			}
			sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "GetSignalChannel" {
				continue
			}
			tv, ok := b.info.Types[c.Args[1]]
			id, isID := as.Lhs[i].(*ast.Ident)
			if ok && tv.Value != nil && tv.Value.Kind() == constant.String && isID {
				if o := b.info.ObjectOf(id); o != nil {
					chans[o] = constant.StringVal(tv.Value)
				}
			}
		}
		return true
	})
	out := map[string]types.Object{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) != 2 {
			return true
		}
		sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "AddReceive" {
			return true
		}
		ch, ok := ast.Unparen(c.Args[0]).(*ast.Ident)
		lit, isLit := c.Args[1].(*ast.FuncLit)
		if !ok || !isLit {
			return true
		}
		name, known := chans[b.info.ObjectOf(ch)]
		if !known {
			return true
		}
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			rc, ok := m.(*ast.CallExpr)
			if !ok || len(rc.Args) < 2 {
				return true
			}
			rs, ok := ast.Unparen(rc.Fun).(*ast.SelectorExpr)
			if !ok || rs.Sel.Name != "Receive" {
				return true
			}
			if u, ok := ast.Unparen(rc.Args[len(rc.Args)-1]).(*ast.UnaryExpr); ok && u.Op == token.AND {
				if id, ok := ast.Unparen(u.X).(*ast.Ident); ok {
					if o := b.info.ObjectOf(id); o != nil {
						out[name] = o
					}
				}
			}
			return true
		})
		return true
	})
	return out
}

// targetOf writes the assignable expression of a value below a root, with its
// root, or says why it cannot.
func (b *builder) targetOf(e ast.Expr, roots map[types.Object]*root) (lhs string, r *root, why string) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		o := b.info.Uses[x]
		if o == nil {
			o = b.info.Defs[x]
		}
		rt := roots[o]
		if rt == nil {
			return "", nil, "it does not read the workflow's input, an activity's result or a signal's payload directly"
		}
		return rt.name, rt, ""
	case *ast.SelectorExpr:
		if sel := b.info.Selections[x]; sel == nil || sel.Kind() != types.FieldVal {
			return "", nil, "it reads a method or a package value"
		}
		l, rt, why := b.targetOf(x.X, roots)
		if why != "" {
			return "", nil, why
		}
		if _, isRoot := ast.Unparen(x.X).(*ast.Ident); !isRoot {
			if _, isPtr := b.info.TypeOf(x.X).Underlying().(*types.Pointer); isPtr {
				return "", nil, "it goes through a pointer field that the test would have to create first"
			}
		}
		return l + "." + x.Sel.Name, rt, ""
	}
	return "", nil, "it reads something other than a field of the input"
}

// atomsOf turns a condition that has the value want into facts.
func (b *builder) atomsOf(cond ast.Expr, want bool) ([]atom, string) {
	cond = ast.Unparen(cond)
	switch x := cond.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return b.atomsOf(x.X, !want)
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			both := (x.Op == token.LAND) == want
			l, why := b.atomsOf(x.X, want)
			if !both {
				if why == "" {
					return l, ""
				}
				return b.atomsOf(x.Y, want) // one side is enough
			}
			if why != "" {
				return nil, why
			}
			r, why := b.atomsOf(x.Y, want)
			if why != "" {
				return nil, why
			}
			return append(l, r...), ""
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return b.compareAtoms(x, want)
		}
	}
	if tv, ok := b.info.Types[cond]; ok && isBool(tv.Type) && tv.Value == nil {
		return []atom{{e: cond, kind: "bool", nil_: want}}, ""
	}
	return nil, "PathKit does not solve this kind of condition"
}

func flipOp(op token.Token) token.Token {
	switch op {
	case token.EQL:
		return token.NEQ
	case token.NEQ:
		return token.EQL
	case token.LSS:
		return token.GEQ
	case token.LEQ:
		return token.GTR
	case token.GTR:
		return token.LEQ
	case token.GEQ:
		return token.LSS
	}
	return op
}

func swapOp(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	}
	return op
}

func (b *builder) compareAtoms(x *ast.BinaryExpr, want bool) ([]atom, string) {
	left, right, op := x.X, x.Y, x.Op
	isConst := func(e ast.Expr) bool { tv, ok := b.info.Types[e]; return ok && (tv.Value != nil || tv.IsNil()) }
	if isConst(left) && !isConst(right) {
		left, right, op = right, left, swapOp(op)
	}
	tv, ok := b.info.Types[right]
	if !ok || !(tv.Value != nil || tv.IsNil()) {
		return nil, "it compares with something that is not a constant"
	}
	if !want {
		op = flipOp(op)
	}
	if tv.IsNil() {
		if op != token.EQL && op != token.NEQ {
			return nil, "it compares nil with an ordering"
		}
		return []atom{{e: left, kind: "nil", nil_: op == token.EQL}}, ""
	}
	// len(x) op number
	if call, ok := ast.Unparen(left).(*ast.CallExpr); ok && len(call.Args) == 1 {
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
			if bi, ok := b.info.Uses[id].(*types.Builtin); ok && bi.Name() == "len" {
				return []atom{{e: call.Args[0], op: op, val: tv.Value, kind: "len"}}, ""
			}
		}
	}
	return []atom{{e: left, op: op, val: tv.Value, kind: "cmp"}}, ""
}

// solveAll works out the values for the whole path.
func (b *builder) solveAll(fn *ast.FuncDecl, pathSteps []stepInfo, params map[types.Object]string) *solved {
	roots := b.rootsOf(fn, params)
	sol := &solved{results: map[types.Object][]assign{}, payloads: map[types.Object][]assign{}, roots: roots}
	order := []string{}
	byKey := map[string]*cons{}
	getCons := func(lhs string, rt *root, typ types.Type) *cons {
		key := lhs
		c := byKey[key]
		if c == nil {
			c = &cons{lhs: lhs, rootName: rt.name, typ: typ}
			byKey[key] = c
			order = append(order, key)
		}
		return c
	}
	rootOfKey := map[string]*root{}
	add := func(a atom, text string, sideWant string, label string) string {
		lhs, rt, why := b.targetOf(a.e, roots)
		if why != "" {
			return why
		}
		typ := b.info.TypeOf(a.e)
		c := getCons(lhs, rt, typ)
		rootOfKey[lhs] = rt
		c.conds = append(c.conds, text+" is "+sideWant)
		b.apply(c, a)
		return ""
	}
	for _, st := range pathSteps {
		switch {
		case st.cond != nil:
			atoms, why := b.atomsOf(st.cond, st.want)
			text := "`" + st.text + "`"
			if why == "" {
				for _, a := range atoms {
					if w := add(a, text, st.side, ""); w != "" {
						why = w
						break
					}
				}
			}
			if why != "" {
				sol.unsolved = append(sol.unsolved, b.unsolvedText(st, why, roots))
			}
		case st.typeSwitch != nil:
			sw := st.typeSwitch
			lhs, rt, why := b.targetOf(sw.x, roots)
			text := "`" + st.text + "` taking " + st.side
			if why != "" {
				sol.unsolved = append(sol.unsolved, fmt.Sprintf("send a value of the type that makes %s (%s)", text, why))
				continue
			}
			if sw.t == nil && !sw.isNil {
				sol.unsolved = append(sol.unsolved, fmt.Sprintf("set %s to a type no other case takes, so that %s", lhs, text))
				continue
			}
			c := getCons(lhs, rt, b.info.TypeOf(sw.x))
			rootOfKey[lhs] = rt
			c.conds = append(c.conds, text)
			c.dyn, c.dynNil = sw.t, sw.isNil
		}
	}
	// values
	for _, key := range order {
		c := byKey[key]
		rt := rootOfKey[key]
		val, why := b.valueFor(c)
		if why != "" {
			sol.unsolved = append(sol.unsolved, fmt.Sprintf("set %s so that %s (%s)", c.lhs, strings.Join(c.conds, " and "), why))
			continue
		}
		a := assign{c.lhs, val}
		switch rt.kind {
		case "input":
			sol.inputs = append(sol.inputs, a)
		case "result":
			sol.results[rt.obj] = append(sol.results[rt.obj], a)
		case "payload":
			sol.payloads[rt.obj] = append(sol.payloads[rt.obj], a)
		}
	}
	return sol
}

// stepInfo is one decision of the path, ready for solving.
type stepInfo struct {
	cond       ast.Expr // a plain if's condition
	want       bool
	text       string // the decision's text
	side       string // the exit's label
	typeSwitch *typeSwitchInfo
}

type typeSwitchInfo struct {
	x     ast.Expr
	t     types.Type // the type the case names; nil for default
	isNil bool
}

func (b *builder) unsolvedText(st stepInfo, why string, roots map[types.Object]*root) string {
	cond := strings.TrimPrefix(st.text, "if ")
	target := "the input"
	if st.cond != nil {
		if atoms, w := b.atomsOf(st.cond, st.want); w == "" && len(atoms) > 0 {
			if lhs, _, tw := b.targetOf(atoms[0].e, roots); tw == "" {
				target = lhs
			}
		}
	}
	return fmt.Sprintf("set %s so that `%s` is %s (%s)", target, cond, st.side, why)
}

// apply adds an atom to a target's constraints.
func (b *builder) apply(c *cons, a atom) {
	switch a.kind {
	case "bool":
		c.boolWant = bptr(a.nil_)
	case "nil":
		c.nilWant = bptr(a.nil_)
	case "len":
		n, ok := constant.Int64Val(constant.ToInt(a.val))
		if !ok {
			c.conflict = "the length is too big for PathKit"
			return
		}
		switch a.op {
		case token.EQL:
			c.minLen, c.maxLen = i64(n), i64(n)
		case token.NEQ:
			if n == 0 {
				c.minLen = maxP(c.minLen, 1)
			} else {
				c.minLen = maxP(c.minLen, 0)
			}
		case token.GTR:
			c.minLen = maxP(c.minLen, n+1)
		case token.GEQ:
			c.minLen = maxP(c.minLen, n)
		case token.LSS:
			c.maxLen = minP(c.maxLen, n-1)
		case token.LEQ:
			c.maxLen = minP(c.maxLen, n)
		}
	case "cmp":
		switch a.op {
		case token.EQL:
			c.eq = append(c.eq, a.val)
		case token.NEQ:
			c.ne = append(c.ne, a.val)
		default:
			n, ok := constant.Int64Val(constant.ToInt(a.val))
			if !ok {
				c.conflict = "the number is too big for PathKit"
				return
			}
			switch a.op {
			case token.GTR:
				c.lo = maxP(c.lo, n+1)
			case token.GEQ:
				c.lo = maxP(c.lo, n)
			case token.LSS:
				c.hi = minP(c.hi, n-1)
			case token.LEQ:
				c.hi = minP(c.hi, n)
			}
		}
	}
}

func maxP(p *int64, v int64) *int64 {
	if p == nil || v > *p {
		return i64(v)
	}
	return p
}

func minP(p *int64, v int64) *int64 {
	if p == nil || v < *p {
		return i64(v)
	}
	return p
}

// valueFor writes a Go expression for a value that satisfies every constraint
// on a target.
func (b *builder) valueFor(c *cons) (string, string) {
	if c.conflict != "" {
		return "", c.conflict
	}
	t := c.typ
	u := t.Underlying()
	conv := func(lit string) string {
		if _, named := t.(*types.Named); named {
			return b.typeString(t) + "(" + lit + ")"
		}
		return lit
	}
	if c.dyn != nil || c.dynNil {
		if _, isIface := u.(*types.Interface); !isIface {
			return "", "the value is not an interface"
		}
		if c.dynNil {
			return "nil", ""
		}
		return b.sampleOf(c.dyn)
	}
	switch tt := u.(type) {
	case *types.Basic:
		switch {
		case tt.Info()&types.IsBoolean != 0:
			if c.boolWant != nil {
				return conv(strconv.FormatBool(*c.boolWant)), ""
			}
			if len(c.eq) > 0 {
				return conv(strconv.FormatBool(constant.BoolVal(c.eq[0]))), ""
			}
			if len(c.ne) > 0 {
				return conv(strconv.FormatBool(!constant.BoolVal(c.ne[0]))), ""
			}
		case tt.Info()&types.IsInteger != 0:
			return b.intValue(c, conv, tt.Info()&types.IsUnsigned != 0)
		case tt.Info()&types.IsFloat != 0:
			if len(c.eq) > 0 {
				f, _ := constant.Float64Val(constant.ToFloat(c.eq[0]))
				return conv(strconv.FormatFloat(f, 'g', -1, 64)), ""
			}
			return b.intValue(c, conv, false)
		case tt.Info()&types.IsString != 0:
			return b.stringValue(c, conv)
		}
	case *types.Pointer:
		if c.nilWant != nil && *c.nilWant {
			return "nil", ""
		}
		if c.nilWant != nil {
			if st, ok := tt.Elem().Underlying().(*types.Struct); ok {
				_ = st
				return "new(" + b.typeString(tt.Elem()) + ")", ""
			}
			return "func() " + b.typeString(t) + " { v := " + b.zero(tt.Elem()) + "; return &v }()", ""
		}
	case *types.Slice:
		if c.nilWant != nil && *c.nilWant {
			return "nil", ""
		}
		min := int64(0)
		if c.minLen != nil {
			min = *c.minLen
		}
		if c.nilWant != nil && !*c.nilWant && min < 1 {
			min = 1
		}
		if c.maxLen != nil && *c.maxLen < min {
			return "", "the length asked for cannot be met"
		}
		if min == 0 && c.minLen != nil {
			return b.typeString(t) + "{}", ""
		}
		if min > 0 {
			if min > 50 {
				return "", "the length asked for is too big for PathKit"
			}
			elems := make([]string, min)
			for i := range elems {
				elems[i] = b.zero(tt.Elem())
			}
			return b.typeString(t) + "{" + strings.Join(elems, ", ") + "}", ""
		}
	case *types.Map:
		if c.nilWant != nil && *c.nilWant {
			return "nil", ""
		}
		min := int64(0)
		if c.minLen != nil {
			min = *c.minLen
		}
		if (c.nilWant != nil && !*c.nilWant) && min == 0 && c.minLen == nil {
			return b.typeString(t) + "{}", ""
		}
		if min > 1 {
			return "", "PathKit writes maps of at most one entry"
		}
		if min == 1 {
			return b.typeString(t) + "{" + b.zero(tt.Key()) + ": " + b.zero(tt.Elem()) + "}", ""
		}
		if c.maxLen != nil && c.minLen != nil {
			return b.typeString(t) + "{}", ""
		}
	case *types.Interface:
		if c.nilWant != nil && *c.nilWant {
			return "nil", ""
		}
	}
	return "", "PathKit does not derive a value of this kind from these conditions"
}

func (b *builder) intValue(c *cons, conv func(string) string, unsigned bool) (string, string) {
	if len(c.eq) > 0 {
		first := c.eq[0]
		for _, o := range c.eq[1:] {
			if !constant.Compare(first, token.EQL, o) {
				return "", "two decisions need different values"
			}
		}
		n, ok := constant.Int64Val(constant.ToInt(first))
		if !ok {
			return "", "the number is too big for PathKit"
		}
		if (c.lo != nil && n < *c.lo) || (c.hi != nil && n > *c.hi) {
			return "", "the value it must equal is outside the range another decision asks for"
		}
		for _, o := range c.ne {
			if constant.Compare(first, token.EQL, o) {
				return "", "two decisions need different values"
			}
		}
		return conv(strconv.FormatInt(n, 10)), ""
	}
	cand := int64(0)
	switch {
	case c.lo != nil:
		cand = *c.lo
	case c.hi != nil && *c.hi < 0 && !unsigned:
		cand = *c.hi
	}
	for tries := 0; tries < 200; tries++ {
		bad := false
		for _, o := range c.ne {
			if n, ok := constant.Int64Val(constant.ToInt(o)); ok && n == cand {
				bad = true
			}
		}
		if !bad {
			break
		}
		cand++
	}
	if (c.hi != nil && cand > *c.hi) || (unsigned && cand < 0) {
		return "", "no value meets the range the decisions ask for"
	}
	return conv(strconv.FormatInt(cand, 10)), ""
}

func (b *builder) stringValue(c *cons, conv func(string) string) (string, string) {
	if len(c.eq) > 0 {
		first := constant.StringVal(c.eq[0])
		for _, o := range c.eq[1:] {
			if constant.StringVal(o) != first {
				return "", "two decisions need different values"
			}
		}
		for _, o := range c.ne {
			if constant.StringVal(o) == first {
				return "", "two decisions need different values"
			}
		}
		if (c.minLen != nil && int64(len(first)) < *c.minLen) || (c.maxLen != nil && int64(len(first)) > *c.maxLen) {
			return "", "the text it must equal has a length another decision does not allow"
		}
		return conv(strconv.Quote(first)), ""
	}
	n := int64(0)
	if c.minLen != nil {
		n = *c.minLen
	}
	if n > 100 {
		return "", "the length asked for is too big for PathKit"
	}
	banned := map[string]bool{}
	for _, o := range c.ne {
		banned[constant.StringVal(o)] = true
	}
	for tries := 0; tries < 50; tries++ {
		s := strings.Repeat("x", int(n))
		if !banned[s] && (c.maxLen == nil || n <= *c.maxLen) {
			return conv(strconv.Quote(s)), ""
		}
		n++
	}
	return "", "no text meets what the decisions ask for"
}

// sampleOf writes a value of type t, for an interface that must hold it.
func (b *builder) sampleOf(t types.Type) (string, string) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			return b.wrapNamed(t, `"x"`), ""
		case u.Info()&types.IsBoolean != 0:
			return b.wrapNamed(t, "true"), ""
		case u.Info()&types.IsNumeric != 0:
			return b.wrapNamed(t, "1"), ""
		}
	case *types.Map, *types.Slice:
		return b.typeString(t) + "{}", ""
	case *types.Pointer:
		return "new(" + b.typeString(u.Elem()) + ")", ""
	case *types.Struct:
		return b.zero(t), ""
	}
	return "", "PathKit does not write a value of the type " + b.typeString(t)
}

// wrapNamed converts a literal to t unless t is the plain basic type itself.
func (b *builder) wrapNamed(t types.Type, lit string) string {
	if bt, ok := t.(*types.Basic); ok {
		switch bt.Kind() {
		case types.Float32, types.Float64:
			return b.typeString(t) + "(" + lit + ")"
		}
		return lit
	}
	return b.typeString(t) + "(" + lit + ")"
}

// sortedAssigns keeps assignments in the order they were found, and writes
// parents before fields (so a struct is set before its parts).
func sortedAssigns(as []assign) []assign {
	out := append([]assign(nil), as...)
	sort.SliceStable(out, func(i, j int) bool { return strings.Count(out[i].lhs, ".") < strings.Count(out[j].lhs, ".") })
	return out
}
