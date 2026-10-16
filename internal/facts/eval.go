package facts

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// tri is a three-valued answer.
type tri int

const (
	unknown tri = iota
	isTrue
	isFalse
)

func (t tri) not() tri {
	switch t {
	case isTrue:
		return isFalse
	case isFalse:
		return isTrue
	}
	return unknown
}

func triOf(b bool) tri {
	if b {
		return isTrue
	}
	return isFalse
}

// operand resolves e to the value it names: a variable, or a field path
// under a variable that holds a struct by value (no pointer on the way).
func (s *state) operand(e ast.Expr) (key, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, _ := s.a.info.ObjectOf(x).(*types.Var)
		if v == nil || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
			return key{}, false
		}
		return key{v: v}, true
	case *ast.SelectorExpr:
		sel := s.a.info.Selections[x]
		if sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
			return key{}, false
		}
		base, ok := s.operand(x.X)
		if !ok {
			return key{}, false
		}
		t := s.a.info.TypeOf(x.X)
		path := base.path
		for _, idx := range sel.Index() {
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				return key{}, false
			}
			f := st.Field(idx)
			path += "." + f.Name()
			t = f.Type()
		}
		return key{v: base.v, path: path}, true
	}
	return key{}, false
}

// constSide reports whether e is a constant (val) or the nil value.
func (s *state) constSide(e ast.Expr) (val constant.Value, isNil, ok bool) {
	tv, has := s.a.info.Types[e]
	if !has {
		return nil, false, false
	}
	if tv.IsNil() {
		return nil, true, true
	}
	// A constant counts only when its value is the same on every machine
	// and in every build (runtime.GOOS, strconv.IntSize and shifts do not).
	if tv.Value != nil && s.a.g.TrustedConstant(e) {
		return tv.Value, false, true
	}
	return nil, false, false
}

func flip(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.GTR:
		return token.LSS
	case token.LEQ:
		return token.GEQ
	case token.GEQ:
		return token.LEQ
	}
	return op
}

// cmpForm is a comparison "operand op constant", or "len(operand) op
// number" for a string operand.
type cmpForm struct {
	k     key
	op    token.Token
	val   constant.Value
	isNil bool
	isLen bool
}

// formOf reads a comparison expression into a cmpForm.
func (s *state) formOf(x *ast.BinaryExpr) (cmpForm, bool) {
	switch x.Op {
	case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
	default:
		return cmpForm{}, false
	}
	l, r, op := x.X, x.Y, x.Op
	if _, _, isConst := s.constSide(l); isConst {
		l, r, op = r, l, flip(op)
	}
	val, isNil, isConst := s.constSide(r)
	if !isConst {
		return cmpForm{}, false
	}
	if _, _, lc := s.constSide(l); lc {
		return cmpForm{}, false // both constant: the compiler's business
	}
	if isNil && op != token.EQL && op != token.NEQ {
		return cmpForm{}, false
	}
	// len(s) for a string s
	if call, ok := ast.Unparen(l).(*ast.CallExpr); ok && len(call.Args) == 1 {
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
			if b, ok := s.a.info.Uses[id].(*types.Builtin); ok && b.Name() == "len" && kindOf(s.a.info.TypeOf(call.Args[0])) == tkString {
				if k, ok := s.operand(call.Args[0]); ok && !isNil && val.Kind() == constant.Int {
					return cmpForm{k: k, op: op, val: val, isLen: true}, true
				}
			}
		}
		return cmpForm{}, false
	}
	k, ok := s.operand(l)
	if !ok {
		return cmpForm{}, false
	}
	switch kindOf(s.a.info.TypeOf(l)) {
	case tkBool, tkString, tkNumber:
		if isNil {
			return cmpForm{}, false
		}
	case tkNilable:
		if !isNil {
			return cmpForm{}, false
		}
	default:
		return cmpForm{}, false
	}
	return cmpForm{k: k, op: op, val: val, isNil: isNil}, true
}

// eval answers a condition from what is known. With shadow, facts PathKit
// has had to forget count too (only the possible-correlation check does
// that). used are the facts the answer rests on.
func (s *state) eval(e ast.Expr, shadow bool) (tri, []*datum) {
	e = ast.Unparen(e)
	if tv, ok := s.a.info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.Bool && s.a.g.TrustedConstant(e) {
		return triOf(constant.BoolVal(tv.Value)), nil
	}
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			t, u := s.eval(x.X, shadow)
			return t.not(), u
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			lt, lu := s.eval(x.X, shadow)
			rt, ru := s.eval(x.Y, shadow)
			switch {
			case lt == isFalse:
				return isFalse, lu
			case rt == isFalse:
				return isFalse, ru
			case lt == isTrue && rt == isTrue:
				return isTrue, append(append([]*datum(nil), lu...), ru...)
			}
			return unknown, nil
		case token.LOR:
			lt, lu := s.eval(x.X, shadow)
			rt, ru := s.eval(x.Y, shadow)
			switch {
			case lt == isTrue:
				return isTrue, lu
			case rt == isTrue:
				return isTrue, ru
			case lt == isFalse && rt == isFalse:
				return isFalse, append(append([]*datum(nil), lu...), ru...)
			}
			return unknown, nil
		}
		if f, ok := s.formOf(x); ok {
			return s.evalForm(f, shadow)
		}
	case *ast.Ident, *ast.SelectorExpr:
		// a bool value used as the condition
		if kindOf(s.a.info.TypeOf(e)) == tkBool {
			if k, ok := s.operand(e); ok {
				for _, d := range s.datums(k, shadow) {
					if d.op == dEq && d.val.Kind() == constant.Bool {
						return triOf(constant.BoolVal(d.val)), []*datum{d}
					}
				}
			}
		}
	}
	return unknown, nil
}

func (s *state) evalForm(f cmpForm, shadow bool) (tri, []*datum) {
	ds := s.datums(f.k, shadow)
	if f.isNil {
		for _, d := range ds {
			switch d.op {
			case dNil:
				return triOf(f.op == token.EQL), []*datum{d}
			case dNonNil:
				return triOf(f.op != token.EQL), []*datum{d}
			}
		}
		return unknown, nil
	}
	if f.isLen {
		for _, d := range ds {
			if d.op == dEq && d.val.Kind() == constant.String {
				n := constant.MakeInt64(int64(len(constant.StringVal(d.val))))
				if r, ok := cmp(n, f.op, f.val); ok {
					return triOf(r), []*datum{d}
				}
			}
		}
		// Known to be not empty: len >= 1.
		for _, d := range ds {
			if d.op == dNe && d.val.Kind() == constant.String && constant.StringVal(d.val) == "" {
				if n, ok := constant.Int64Val(f.val); ok {
					switch {
					case f.op == token.EQL && n == 0, f.op == token.LSS && n <= 1, f.op == token.LEQ && n <= 0:
						return isFalse, []*datum{d}
					case f.op == token.NEQ && n == 0, f.op == token.GTR && n <= 0, f.op == token.GEQ && n <= 1:
						return isTrue, []*datum{d}
					}
				}
			}
		}
		return unknown, nil
	}
	for _, d := range ds {
		if d.op == dEq {
			if r, ok := cmp(d.val, f.op, f.val); ok {
				return triOf(r), []*datum{d}
			}
		}
	}
	if f.op == token.EQL || f.op == token.NEQ {
		for _, d := range ds {
			if d.op == dNe && cmpEq(d.val, f.val) {
				return triOf(f.op == token.NEQ), []*datum{d}
			}
		}
	}
	return unknown, nil
}
