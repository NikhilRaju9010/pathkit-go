package facts

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// run executes the statements a road runs, in order.
func (s *state) run(nodes []ast.Node) {
	for _, n := range nodes {
		if m, ok := n.(model.SkipMark); ok {
			s.skip(m)
			continue
		}
		if m, ok := n.(model.EnterMark); ok {
			s.enter(m)
			continue
		}
		if s.inSkip(n.Pos()) {
			continue // inside an if or loop PathKit did not decide
		}
		s.exec(n)
	}
}

// skip handles a SkipMark: whatever the statement assigns is unknown from
// here on, and its code is not run by this walk.
func (s *state) skip(m model.SkipMark) {
	_, line := s.a.baseFile(m.Stmt.Pos())
	reason := fmt.Sprintf("the if or loop at line %d, which PathKit does not follow step by step, may change it", line)
	for v := range s.a.assignedVars(m.Stmt) {
		s.forgetVar(v, reason)
	}
	s.skips = append(s.skips, span{m.Stmt.Pos(), m.Stmt.End()})
}

// enter handles an EnterMark: the walk goes into a copy of a helper's code.
// Whatever an earlier copy left in the helper's variables (its parameters
// and locals) is gone, and when the call itself was in code this walk does
// not follow, so is the helper's.
func (s *state) enter(m model.EnterMark) {
	from, to := m.Site.Type.Pos(), m.Site.Body.End()
	if m.Site.Decl != nil {
		from = m.Site.Decl.Pos()
	}
	for k := range s.d {
		if k.v != nil && k.v.Pos() >= from && k.v.Pos() < to {
			s.killKey(key{v: k.v})
		}
	}
	for k, l := range s.links {
		if (k.v != nil && k.v.Pos() >= from && k.v.Pos() < to) || (l.v.v != nil && l.v.v.Pos() >= from && l.v.v.Pos() < to) {
			delete(s.links, k)
		}
	}
	if s.inSkip(m.Site.Call.Pos()) {
		s.skips = append(s.skips, span{from, to})
	}
}

func (s *state) exec(n ast.Node) {
	s.callEffects(n)
	s.decodeEffects(n)
	switch x := n.(type) {
	case *ast.AssignStmt:
		s.execAssign(x)
	case *ast.ValueSpec: // var x T, var x = ... (go/cfg lists the spec itself)
		s.execValueSpec(x)
	case *ast.DeclStmt:
		if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					s.execValueSpec(vs)
				}
			}
		}
	case *ast.IncDecStmt:
		s.storeIndex(x.X, nil, x.Tok)
		s.killLHS(x.X)
	case *ast.RangeStmt:
		if x.Key != nil {
			s.storeIndex(x.Key, nil, token.ASSIGN)
			s.killLHS(x.Key)
		}
		if x.Value != nil {
			s.storeIndex(x.Value, nil, token.ASSIGN)
			s.killLHS(x.Value)
		}
	}
}

// callEffects: a call, or an address-of, in this node may write a value the
// engine never trusts (its address is taken, or a closure writes it). Those
// values keep only "forgotten" notes for the possible-correlation check, and
// a call in between ends such a note: the value may well have changed, so
// there is no correlation to suspect. (Values the engine does trust cannot
// be written by a call.)
func (s *state) callEffects(n ast.Node) {
	if !s.a.mayCall(n) {
		return
	}
	for k := range s.d {
		if _, bad := s.a.untracked[k.v]; bad {
			delete(s.d, k)
		}
	}
}

// mayCall reports whether the node holds a call (other than a conversion or
// a builtin) or an address-of. It is asked once per node and remembered:
// the same statements are walked again for every path.
func (a *Analyzer) mayCall(n ast.Node) bool {
	if v, ok := a.calls[n]; ok {
		return v
	}
	hasCall := false
	ast.Inspect(n, func(c ast.Node) bool {
		switch x := c.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if tv, ok := a.info.Types[x.Fun]; ok && tv.IsType() {
				return true // a conversion
			}
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				if _, isBuiltin := a.info.Uses[id].(*types.Builtin); isBuiltin {
					return true
				}
			}
			hasCall = true
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				hasCall = true
			}
		}
		return true
	})
	a.calls[n] = hasCall
	return hasCall
}

// killLHS: the value on the left of an assignment gets a new value.
func (s *state) killLHS(l ast.Expr) {
	if k, ok := s.operand(l); ok {
		s.killKey(k)
	}
}

// spec is a fact to give a value (relative to the value's key: sub is the
// field path below it).
type spec struct {
	sub string
	d   datum
}

// derive works out what is known about the value of rhs, as facts for a
// variable of type lt. Nothing is known for most expressions.
func (s *state) derive(rhs ast.Expr, lt types.Type) []spec {
	info := s.a.info
	lk := kindOf(lt)
	tv := info.Types[rhs]
	if !tv.IsNil() && (isInterface(lt) || isMapOrSlice(lt)) {
		if out := s.deriveTypes(rhs, lt); len(out) > 0 {
			return append(out, s.deriveNilness(rhs, lt)...)
		}
	}
	switch {
	case tv.IsNil():
		if lk == tkNilable {
			w := s.newWhy(oNil, rhs.Pos(), key{}, lit("is set to nil"))
			return []spec{{d: datum{op: dNil, why: w}}}
		}
		return nil
	case tv.Value != nil:
		if !s.a.g.TrustedConstant(rhs) {
			return nil
		}
		if lk == tkBool || lk == tkString || lk == tkNumber {
			w := s.newWhy(oConst, rhs.Pos(), key{}, func() string { return fmt.Sprintf("is set to %s", tv.Value.ExactString()) })
			return []spec{{d: datum{op: dEq, val: tv.Value, why: w}}}
		}
		return nil
	}
	e := ast.Unparen(rhs)
	nonNil := func() []spec {
		if lk != tkNilable {
			return nil
		}
		w := s.newWhy(oNonNil, rhs.Pos(), key{}, lit(neverNil))
		return []spec{{d: datum{op: dNonNil, why: w}}}
	}
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if _, ok := ast.Unparen(x.X).(*ast.CompositeLit); ok {
				return nonNil()
			}
		}
	case *ast.CompositeLit:
		switch info.TypeOf(x).Underlying().(type) {
		case *types.Slice, *types.Map:
			return nonNil()
		}
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok && (b.Name() == "new" || b.Name() == "make") {
				return nonNil()
			}
		}
	}
	// a copy of another tracked value of the same type
	if rk, ok := s.operand(rhs); ok && types.Identical(lt, info.TypeOf(rhs)) {
		var out []spec
		for kk, ds := range s.d {
			if kk.v != rk.v || !underPath(kk.path, rk.path) {
				continue
			}
			sub := kk.path[len(rk.path):]
			for _, d := range ds {
				w := s.newWhy(oCopy, rhs.Pos(), key{}, func() string { return fmt.Sprintf("is a copy of `%s`", exprText(rhs)) }, d.why)
				cp := datum{op: d.op, val: d.val, keys: d.keys, why: w, soft: d.soft}
				if d.ts != nil {
					cp.ts = d.ts.clone()
				}
				out = append(out, spec{sub: sub, d: cp})
			}
		}
		return out
	}
	return nil
}

// deriveNilness: a composite literal, make or &T{} is never nil.
func (s *state) deriveNilness(rhs ast.Expr, lt types.Type) []spec {
	if kindOf(lt) != tkNilable || isInterface(lt) {
		return nil
	}
	switch x := ast.Unparen(rhs).(type) {
	case *ast.CompositeLit:
		w := s.newWhy(oNonNil, rhs.Pos(), key{}, lit(neverNil))
		return []spec{{d: datum{op: dNonNil, why: w}}}
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if b, ok := s.a.info.Uses[id].(*types.Builtin); ok && b.Name() == "make" {
				w := s.newWhy(oNonNil, rhs.Pos(), key{}, lit(neverNil))
				return []spec{{d: datum{op: dNonNil, why: w}}}
			}
		}
	}
	return nil
}

// apply gives the value k the facts in specs.
func (s *state) apply(k key, specs []spec) {
	for _, sp := range specs {
		d := sp.d
		kk := key{v: k.v, path: k.path + sp.sub}
		d.why.key = kk
		s.add(kk, &d)
	}
}

func (s *state) execAssign(x *ast.AssignStmt) {
	if x.Tok != token.ASSIGN && x.Tok != token.DEFINE { // x += ...
		for _, l := range x.Lhs {
			s.storeIndex(l, nil, x.Tok)
			s.killLHS(l)
		}
		return
	}
	if s.appendSelf(x) {
		return
	}
	if len(x.Lhs) == len(x.Rhs) {
		// All right-hand sides are read before any left-hand side changes
		// (a, b = b, a).
		all := make([][]spec, len(x.Rhs))
		for i, r := range x.Rhs {
			if lt := s.a.info.TypeOf(x.Lhs[i]); lt != nil {
				all[i] = s.derive(r, lt)
			}
		}
		for i, l := range x.Lhs {
			s.storeIndex(l, x.Rhs[i], token.ASSIGN)
			s.killLHS(l)
			if k, ok := s.operand(l); ok {
				s.apply(k, all[i])
			}
		}
		return
	}
	// v, ok := x.(T)   v, ok := m[k]   (and calls with several results)
	for _, l := range x.Lhs {
		s.storeIndex(l, nil, token.ASSIGN) // m["a"], err = f(): the element is not understood
		s.killLHS(l)
	}
	if len(x.Lhs) == 2 && len(x.Rhs) == 1 {
		switch r := ast.Unparen(x.Rhs[0]).(type) {
		case *ast.TypeAssertExpr:
			s.commaOk(x.Lhs[0], x.Lhs[1], x.Rhs[0])
			s.assertion(x.Lhs[0], x.Lhs[1], x.Rhs[0])
		case *ast.IndexExpr:
			if _, isMap := s.a.info.TypeOf(r.X).Underlying().(*types.Map); isMap {
				s.commaOk(x.Lhs[0], x.Lhs[1], x.Rhs[0])
				// the value is one of the elements, or nil when the key is missing
				if vk, okv := s.operand(x.Lhs[0]); okv && s.a.trackable(vk.v) && isInterface(s.a.info.TypeOf(x.Lhs[0])) {
					if ts, used, known := s.elemTypes(r); known {
						d := s.typesDatum(ts, r.Pos(), key{})
						for _, u := range used {
							d.why.parents = append(d.why.parents, datumWhys(u)...)
						}
						s.apply(vk, []spec{{d: *d}})
					}
				}
			}
		}
	}
}

// commaOk remembers that when ok is false, v holds the zero value.
func (s *state) commaOk(vExpr, okExpr, rhs ast.Expr) {
	vk, vok := s.operand(vExpr)
	ok, okok := s.operand(okExpr)
	if !vok || !okok || !s.a.trackable(vk.v) || !s.a.trackable(ok.v) || kindOf(s.a.info.TypeOf(okExpr)) != tkBool {
		return
	}
	if id, isIdent := ast.Unparen(vExpr).(*ast.Ident); isIdent && id.Name == "_" {
		return
	}
	w := s.newWhy(oCommaOk, rhs.Pos(), vk, func() string {
		return fmt.Sprintf("`%s, %s := %s`: when `%s` is false, `%s` holds the zero value of its type", exprText(vExpr), exprText(okExpr), exprText(rhs), exprText(okExpr), exprText(vExpr))
	})
	s.links[ok] = link{v: vk, ty: s.a.info.TypeOf(vExpr), by: w}
}

func (s *state) execValueSpec(vs *ast.ValueSpec) {
	switch {
	case len(vs.Values) == 0: // var x T
		for _, name := range vs.Names {
			v, _ := s.a.info.Defs[name].(*types.Var)
			if v == nil || name.Name == "_" {
				continue
			}
			k := key{v: v}
			s.killKey(k)
			s.zero(k, v.Type(), name.Pos(), 0)
		}
	case len(vs.Values) == len(vs.Names):
		all := make([][]spec, len(vs.Values))
		for i, r := range vs.Values {
			if v, _ := s.a.info.Defs[vs.Names[i]].(*types.Var); v != nil {
				all[i] = s.derive(r, v.Type())
			}
		}
		for i, name := range vs.Names {
			v, _ := s.a.info.Defs[name].(*types.Var)
			if v == nil || name.Name == "_" {
				continue
			}
			s.killKey(key{v: v})
			s.apply(key{v: v}, all[i])
		}
	default: // var a, b = f()
		for _, name := range vs.Names {
			if v, _ := s.a.info.Defs[name].(*types.Var); v != nil {
				s.killKey(key{v: v})
			}
		}
	}
}

// zero records that k holds the zero value of its type (declared, never
// assigned): "", 0, false, nil, and the same for the fields of a struct.
func (s *state) zero(k key, t types.Type, pos token.Pos, depth int) {
	switch kindOf(t) {
	case tkBool, tkString, tkNumber:
		w := s.newWhy(oDecl, pos, k, func() string {
			return fmt.Sprintf("starts as %s, the zero value of its type", zeroConst(t).ExactString())
		})
		s.add(k, &datum{op: dEq, val: zeroConst(t), why: w})
	case tkNilable:
		w := s.newWhy(oDecl, pos, k, lit("starts as nil"))
		s.add(k, &datum{op: dNil, why: w})
	case tkStruct:
		if depth >= 3 {
			return
		}
		st := t.Underlying().(*types.Struct)
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			s.zero(key{v: k.v, path: k.path + "." + f.Name()}, f.Type(), pos, depth+1)
		}
	}
}

// ---- decisions ----

// stepInfo describes the decision being taken, for the sentences of a proof.
type stepInfo struct {
	step  int
	where string // "J2 (if id == \"\")"
	side  string // "true" or "false"
	pos   token.Pos
	early bool
}

// learn adds what taking the given side of cond tells about the values in it.
func (s *state) learn(e ast.Expr, taken bool, si stepInfo) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			s.learn(x.X, !taken, si)
		}
		return
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			if taken {
				s.learn(x.X, true, si)
				s.learn(x.Y, true, si)
			}
			return
		case token.LOR:
			if !taken {
				s.learn(x.X, false, si)
				s.learn(x.Y, false, si)
			}
			return
		}
		if f, ok := s.formOf(x); ok {
			s.learnForm(f, taken, si)
		}
		return
	case *ast.Ident, *ast.SelectorExpr:
		if kindOf(s.a.info.TypeOf(e)) == tkBool {
			if k, ok := s.operand(e); ok {
				s.addEq(k, constant.MakeBool(taken), si)
			}
		}
	}
}

func (s *state) branchWhy(k key, si stepInfo, statement func() string) *why {
	w := s.newWhy(oBranch, si.pos, k, func() string { return fmt.Sprintf("%s takes the %s side, so %s", si.where, si.side, statement()) })
	w.step, w.early = si.step, si.early
	return w
}

func (s *state) addEq(k key, val constant.Value, si stepInfo) {
	w := s.branchWhy(k, si, func() string { return fmt.Sprintf("`%s` is %s.", k, val.ExactString()) })
	s.add(k, &datum{op: dEq, val: val, why: w})
	s.applyLink(k, val, si)
}

// applyLink: ok became false, so the value it guards holds its zero value.
func (s *state) applyLink(k key, val constant.Value, si stepInfo) {
	l, ok := s.links[k]
	if !ok || val.Kind() != constant.Bool || constant.BoolVal(val) {
		return
	}
	w := s.newWhy(oCommaOk, si.pos, l.v, func() string {
		return fmt.Sprintf("%s takes the %s side, so `%s` is false and `%s` holds the zero value of its type.", si.where, si.side, k, l.v)
	}, l.by)
	w.step = si.step
	s.zeroWith(l.v, l.ty, w, 0)
}

// zeroWith is zero with a given reason.
func (s *state) zeroWith(k key, t types.Type, w *why, depth int) {
	switch kindOf(t) {
	case tkBool, tkString, tkNumber:
		s.add(k, &datum{op: dEq, val: zeroConst(t), why: w})
	case tkNilable:
		s.add(k, &datum{op: dNil, why: w})
	case tkStruct:
		if depth >= 3 {
			return
		}
		st := t.Underlying().(*types.Struct)
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			s.zeroWith(key{v: k.v, path: k.path + "." + f.Name()}, f.Type(), w, depth+1)
		}
	}
}

func (s *state) learnForm(f cmpForm, taken bool, si stepInfo) {
	holds := (f.op == token.EQL || f.op == token.NEQ) && ((f.op == token.EQL) == taken) // the equality is true
	switch {
	case f.elem != nil:
		return // nothing learned about an element
	case f.isNil:
		if f.op != token.EQL && f.op != token.NEQ {
			return
		}
		if !holds {
			s.refineTypes(f.k, si)
		}
		if holds {
			s.add(f.k, &datum{op: dNil, why: s.branchWhy(f.k, si, func() string { return fmt.Sprintf("`%s` is nil.", f.k) })})
		} else {
			s.add(f.k, &datum{op: dNonNil, why: s.branchWhy(f.k, si, func() string { return fmt.Sprintf("`%s` is not nil.", f.k) })})
		}
	case f.isLen:
		n, ok := constant.Int64Val(f.val)
		if !ok {
			return
		}
		emptyString := constant.MakeString("")
		switch {
		case f.op == token.EQL && n == 0 && taken, f.op == token.NEQ && n == 0 && !taken,
			f.op == token.LSS && n == 1 && taken, f.op == token.LEQ && n == 0 && taken,
			f.op == token.GTR && n == 0 && !taken, f.op == token.GEQ && n == 1 && !taken:
			if f.coll {
				s.add(f.k, &datum{op: dLenZero, why: s.branchWhy(f.k, si, func() string { return fmt.Sprintf("`%s` is empty.", f.k) })})
			} else {
				s.addEqString(f.k, emptyString, si)
			}
		case f.op == token.EQL && n == 0 && !taken, f.op == token.NEQ && n == 0 && taken,
			f.op == token.GTR && n == 0 && taken, f.op == token.GEQ && n == 1 && taken,
			f.op == token.LSS && n == 1 && !taken, f.op == token.LEQ && n == 0 && !taken:
			op := dNe
			if f.coll {
				op = dLenNonZero
			}
			s.add(f.k, &datum{op: op, val: emptyString, why: s.branchWhy(f.k, si, func() string { return fmt.Sprintf("`%s` is not empty.", f.k) })})
		}
	case f.op == token.EQL || f.op == token.NEQ:
		if holds {
			s.addEq(f.k, f.val, si)
		} else {
			s.add(f.k, &datum{op: dNe, val: f.val, why: s.branchWhy(f.k, si, func() string { return fmt.Sprintf("`%s` is not %s.", f.k, f.val.ExactString()) })})
		}
	}
}

func (s *state) addEqString(k key, val constant.Value, si stepInfo) {
	w := s.branchWhy(k, si, func() string { return fmt.Sprintf("`%s` is empty.", k) })
	s.add(k, &datum{op: dEq, val: val, why: w})
}

// terminates: does the block end the function or the loop round (so the
// code after the if only runs on the other side)?
func terminates(b ast.Stmt) bool {
	blk, ok := b.(*ast.BlockStmt)
	if !ok || len(blk.List) == 0 {
		return false
	}
	switch last := blk.List[len(blk.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if c, ok := last.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	}
	return false
}

// refineTypes: a value was found not to be nil, so it is not nil in the set
// of types it may hold either.
func (s *state) refineTypes(k key, si stepInfo) {
	for _, d := range s.d[k] {
		if d.op == dTypes && d.soft == "" && d.ts.mayNil {
			ts := d.ts.clone()
			ts.mayNil, ts.nilWhy = false, nil
			w := s.branchWhy(k, si, func() string { return fmt.Sprintf("`%s` is not nil.", k) })
			w.parents = append(w.parents, datumWhys(d)...)
			s.add(k, &datum{op: dTypes, ts: ts, why: w})
			return
		}
	}
}

// neverNil is the reason for a fact about a value built by &T{}, new, make or a literal.
const neverNil = "is set to a new value (&T{}, new, make or a literal), which is never nil"
