package facts

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// typeName writes a type the way a person reads it in the code.
func (a *Analyzer) typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == a.g.Types() {
			return ""
		}
		return p.Name()
	})
}

// listTypes writes "`string`, `*string`" (and "nil").
func (a *Analyzer) listTypes(ts *typeSet) string {
	var parts []string
	for _, e := range ts.ents {
		parts = append(parts, "`"+a.typeName(e.t)+"`")
	}
	if ts.mayNil {
		parts = append(parts, "nil")
	}
	return strings.Join(parts, ", ")
}

// valueTypes works out the dynamic types the value of e can have at run
// time, when that is known:
//   - a value of a concrete static type has exactly that type;
//   - a variable that holds an interface has what was recorded for it;
//   - an element of a slice or map built in this function has one of the
//     recorded element types (for a map, nil too unless the key is proven
//     to be in it).
//
// used are the facts the answer rests on.
func (s *state) valueTypes(e ast.Expr) (ts *typeSet, used []*datum, ok bool) {
	e = ast.Unparen(e)
	tv, has := s.a.info.Types[e]
	if !has || tv.IsNil() {
		return nil, nil, false
	}
	if concrete(tv.Type) {
		t := tv.Type
		if tv.Value != nil {
			t = types.Default(t)
		}
		w := s.newWhy(oStore, e.Pos(), key{}, func() string { return fmt.Sprintf("the value `%s` is a `%s`", exprText(e), s.a.typeName(t)) })
		set := &typeSet{}
		set.add(t, w)
		return set, nil, true
	}
	if !isInterface(tv.Type) {
		return nil, nil, false
	}
	if k, ok := s.operand(e); ok {
		for _, d := range s.datums(k, false) {
			if d.op == dTypes {
				return d.ts.clone(), []*datum{d}, true
			}
		}
		return nil, nil, false
	}
	if ix, isIndex := e.(*ast.IndexExpr); isIndex {
		return s.elemTypes(ix)
	}
	return nil, nil, false
}

// elemTypes: the types of coll[i] for a slice or map coll held in a tracked
// variable that never escapes.
func (s *state) elemTypes(ix *ast.IndexExpr) (*typeSet, []*datum, bool) {
	ck, ok := s.operand(ix.X)
	if !ok || s.a.escapes[ck.v] != "" {
		return nil, nil, false
	}
	var elem, keys *datum
	for _, d := range s.datums(ck, false) {
		switch d.op {
		case dElem:
			elem = d
		case dKeys:
			keys = d
		}
	}
	if elem == nil {
		return nil, nil, false
	}
	set := elem.ts.clone()
	used := []*datum{elem}
	if isMap(s.a.info.TypeOf(ix.X)) {
		proven := false
		// Only a map with plain keys (string, number, bool): a map[interface{}]
		// treats 1, 1.0 and int64(1) as three different keys.
		if kv, _, isConst := s.constSide(ix.Index); isConst && keys != nil && kv != nil && plainKeys(s.a.info.TypeOf(ix.X)) {
			for _, have := range keys.keys {
				if cmpEq(have, kv) {
					proven = true
				}
			}
			if proven {
				used = append(used, keys)
			}
		}
		if !proven {
			w := s.newWhy(oElem, ix.Pos(), ck, func() string {
				return fmt.Sprintf("`%s` may not have that key, and a missing key gives nil", exprText(ix.X))
			})
			set.addNil(w)
		}
	}
	return set, used, true
}

// typesDatum makes the dTypes fact for an interface value that holds ts.
func (s *state) typesDatum(ts *typeSet, pos token.Pos, k key) *datum {
	w := s.newWhy(oStore, pos, k, func() string { return "holds " + s.a.holdsText(ts) })
	return &datum{op: dTypes, ts: ts, why: w}
}

// holdsText: "only a `string` or a `*string`".
func (a *Analyzer) holdsText(ts *typeSet) string {
	if len(ts.ents) == 0 && ts.mayNil {
		return "only nil"
	}
	return "only " + a.listTypes(ts)
}

// ---- declaring and assigning slices, maps and interface values ----

// deriveTypes gives the facts about types, lengths and keys that follow from
// assigning rhs to a variable of type lt.
func (s *state) deriveTypes(rhs ast.Expr, lt types.Type) []spec {
	e := ast.Unparen(rhs)
	if ac, ok := s.assertedJSONContainer(e); ok {
		return ac
	}
	switch {
	case isInterface(lt):
		if ts, used, ok := s.valueTypes(e); ok {
			d := s.typesDatum(ts, e.Pos(), key{})
			for _, u := range used {
				d.why.parents = append(d.why.parents, datumWhys(u)...)
			}
			return []spec{{d: *d}}
		}
	case isMapOrSlice(lt):
		return s.deriveCollection(e, lt)
	}
	return nil
}

// plainKeys: a map whose key type is a basic type (string, number, bool), so
// a constant key has exactly one meaning. An interface key does not.
func plainKeys(t types.Type) bool {
	m, ok := t.Underlying().(*types.Map)
	if !ok {
		return false
	}
	_, basic := m.Key().Underlying().(*types.Basic)
	return basic
}

func isMapOrSlice(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Map, *types.Slice:
		return true
	}
	return false
}

// deriveCollection gives the facts about a slice or map that a composite
// literal or make creates: its length, the types of its elements (for
// interface elements) and the constant keys of a map.
func (s *state) deriveCollection(e ast.Expr, lt types.Type) []spec {
	elemT, _ := elemOf(lt)
	switch x := e.(type) {
	case *ast.CompositeLit:
		var out []spec
		n := len(x.Elts)
		w := s.newWhy(oLen, x.Pos(), key{}, func() string {
			if n == 0 {
				return "is created empty"
			}
			return fmt.Sprintf("is created with %d element(s)", n)
		})
		if n == 0 {
			out = append(out, spec{d: datum{op: dLenZero, why: w}})
		} else {
			out = append(out, spec{d: datum{op: dLenNonZero, why: w}})
		}
		sparse := false
		if !isMap(lt) { // []T{2: x}: the slots before index 2 hold the zero value
			for _, el := range x.Elts {
				if _, isKV := el.(*ast.KeyValueExpr); isKV {
					sparse = true
				}
			}
		}
		if isInterface(elemT) && !sparse {
			ts := &typeSet{}
			ok := true
			var keys []constant.Value
			for _, el := range x.Elts {
				val := el
				if kv, isKV := el.(*ast.KeyValueExpr); isKV {
					val = kv.Value
					if c, _, isConst := s.constSide(kv.Key); isConst && c != nil && isMap(lt) && plainKeys(lt) {
						keys = append(keys, c)
					}
				}
				if !s.addValueTypes(ts, val, x.Pos()) {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, spec{d: datum{op: dElem, ts: ts, why: s.newWhy(oStore, x.Pos(), key{}, lit("is built here"))}})
				if len(keys) > 0 {
					out = append(out, spec{d: datum{op: dKeys, keys: keys, why: w}})
				}
			}
		}
		return out
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if b, ok := s.a.info.Uses[id].(*types.Builtin); ok && b.Name() == "make" {
				return s.deriveMake(x, lt, elemT)
			}
		}
	}
	return nil
}

func (s *state) deriveMake(call *ast.CallExpr, lt, elemT types.Type) []spec {
	var out []spec
	nonEmpty := false
	empty := false
	if isMap(lt) {
		empty = true
	} else if len(call.Args) >= 2 {
		if c, _, ok := s.constSide(call.Args[1]); ok && c != nil && c.Kind() == constant.Int {
			if n, exact := constant.Int64Val(c); exact {
				nonEmpty, empty = n > 0, n == 0
			}
		}
	}
	w := s.newWhy(oLen, call.Pos(), key{}, func() string { return "is made here" })
	switch {
	case nonEmpty:
		out = append(out, spec{d: datum{op: dLenNonZero, why: w}})
	case empty:
		out = append(out, spec{d: datum{op: dLenZero, why: w}})
	}
	if isInterface(elemT) && (empty || nonEmpty) {
		ts := &typeSet{}
		if nonEmpty { // make([]interface{}, 3) holds three nils
			ts.addNil(s.newWhy(oStore, call.Pos(), key{}, lit("is made with nil elements")))
		}
		out = append(out, spec{d: datum{op: dElem, ts: ts, why: s.newWhy(oStore, call.Pos(), key{}, lit("is made here"))}})
	}
	return out
}

// addValueTypes adds the types the value expression may have to ts; false
// when they are not known (then nothing is known about the collection).
func (s *state) addValueTypes(ts *typeSet, val ast.Expr, pos token.Pos) bool {
	val = ast.Unparen(val)
	if tv, ok := s.a.info.Types[val]; ok && tv.IsNil() {
		ts.addNil(s.newWhy(oStore, val.Pos(), key{}, lit("is given nil")))
		return true
	}
	vts, _, ok := s.valueTypes(val)
	if !ok {
		return false
	}
	ts.union(vts)
	return true
}

// ---- stores through an index, and append ----

// storeIndex handles coll[i] = v (and the other assignments to coll[i]).
// It runs before the assignment changes anything.
func (s *state) storeIndex(l ast.Expr, rhs ast.Expr, tok token.Token) {
	ix, ok := ast.Unparen(l).(*ast.IndexExpr)
	if !ok {
		return
	}
	ck, ok := s.operand(ix.X)
	if !ok || s.a.escapes[ck.v] != "" || !s.a.trackable(ck.v) {
		return
	}
	collT := s.a.info.TypeOf(ix.X)
	var elem, keys *datum
	for _, d := range s.datums(ck, false) {
		switch d.op {
		case dElem:
			elem = d
		case dKeys:
			keys = d
		}
	}
	// the element types
	if elem != nil {
		ts := elem.ts.clone()
		var parents []*why
		parents = append(parents, datumWhys(elem)...)
		if tok == token.ASSIGN && rhs != nil && s.addValueTypes(ts, rhs, l.Pos()) {
			w := s.newWhy(oStore, l.Pos(), ck, func() string { return fmt.Sprintf("receives a value in `%s`", exprText(l)) }, parents...)
			s.d[ck] = withoutOp(s.d[ck], dElem)
			s.d[ck] = append(s.d[ck], &datum{op: dElem, ts: ts, why: w})
		} else {
			s.d[ck] = withoutOp(s.d[ck], dElem) // not understood: forget
		}
	}
	if isMap(collT) {
		// after a store the map has an entry; the key is in it
		w := s.newWhy(oLen, l.Pos(), ck, func() string { return fmt.Sprintf("receives a value in `%s`", exprText(l)) })
		s.d[ck] = withoutOp(withoutOp(s.d[ck], dLenZero), dLenNonZero)
		s.d[ck] = append(s.d[ck], &datum{op: dLenNonZero, why: w})
		if c, _, isConst := s.constSide(ix.Index); isConst && c != nil && plainKeys(collT) {
			var have []constant.Value
			if keys != nil {
				have = append(have, keys.keys...)
			}
			have = append(have, c)
			s.d[ck] = withoutOp(s.d[ck], dKeys)
			s.d[ck] = append(s.d[ck], &datum{op: dKeys, keys: have, why: w})
		}
	}
}

func withoutOp(ds []*datum, op dop) []*datum {
	var out []*datum
	for _, d := range ds {
		if d.op != op {
			out = append(out, d)
		}
	}
	return out
}

// datumWhys lists the reasons behind a fact.
func datumWhys(d *datum) []*why {
	var out []*why
	if d.ts != nil {
		out = append(out, d.ts.whys()...)
	}
	if d.why != nil {
		out = append(out, d.why)
	}
	return out
}

// appendSelf handles "s = append(s, a, b)" for a slice held in a variable
// that never escapes. It returns false when the statement is not that form.
func (s *state) appendSelf(x *ast.AssignStmt) bool {
	if len(x.Lhs) != 1 || len(x.Rhs) != 1 || x.Tok != token.ASSIGN {
		return false
	}
	call, ok := ast.Unparen(x.Rhs[0]).(*ast.CallExpr)
	if !ok || len(call.Args) < 1 {
		return false
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); !ok {
		return false
	} else if b, ok := s.a.info.Uses[id].(*types.Builtin); !ok || b.Name() != "append" {
		return false
	}
	lk, ok1 := s.operand(x.Lhs[0])
	ak, ok2 := s.operand(call.Args[0])
	if !ok1 || !ok2 || lk != ak {
		return false
	}
	if s.a.escapes[lk.v] != "" || !s.a.trackable(lk.v) {
		return true // nothing is known about it; the statement changes nothing we track
	}
	var elem *datum
	for _, d := range s.datums(lk, false) {
		if d.op == dElem {
			elem = d
		}
	}
	elemT, _ := elemOf(s.a.info.TypeOf(x.Lhs[0]))
	added := len(call.Args) - 1
	spread := call.Ellipsis != token.NoPos
	var ts *typeSet
	var parents []*why
	if elem != nil && isInterface(elemT) && !spread {
		ts = elem.ts.clone()
		parents = append(parents, datumWhys(elem)...)
		for _, arg := range call.Args[1:] {
			if !s.addValueTypes(ts, arg, x.Pos()) {
				ts = nil
				break
			}
		}
	}
	s.killKey(lk)
	w := s.newWhy(oLen, x.Pos(), lk, func() string { return "gets an element by append" })
	if added > 0 || spread {
		if !spread {
			s.d[lk] = append(s.d[lk], &datum{op: dLenNonZero, why: w})
		}
		s.d[lk] = append(s.d[lk], &datum{op: dNonNil, why: w})
	}
	if ts != nil {
		w2 := s.newWhy(oStore, x.Pos(), lk, func() string { return "receives values by append" }, parents...)
		s.d[lk] = append(s.d[lk], &datum{op: dElem, ts: ts, why: w2})
	}
	return true
}

// ---- type assertions ----

// assertion handles "v, ok := x.(T)" for a value whose possible types are
// known: ok is then decided. It also gives a container found inside a
// decoded value its element types. It returns the facts for the new
// variables, to be applied after the assignment.
func (s *state) assertion(vExpr, okExpr, rhs ast.Expr) {
	ta, isAssert := ast.Unparen(rhs).(*ast.TypeAssertExpr)
	if !isAssert || ta.Type == nil {
		return
	}
	okKey, okIs := s.operand(okExpr)
	if okIs && s.a.trackable(okKey.v) {
		if ts, used, known := s.valueTypes(ta.X); known && (len(ts.ents) > 0 || ts.mayNil) {
			T := s.a.info.TypeOf(ta.Type)
			if T != nil {
				all, none := !ts.mayNil, true
				for _, e := range ts.ents {
					if matchesCase(e.t, T) {
						none = false
					} else {
						all = false
					}
				}
				if len(ts.ents) == 0 {
					all = false
				}
				if all || none {
					val := all
					w := s.newWhy(oTypeAssert, rhs.Pos(), okKey, func() string {
						if val {
							return fmt.Sprintf("`%s` is true: `%s` holds %s, and every one of them is a `%s`", exprText(okExpr), exprText(ta.X), s.a.holdsText(ts), s.a.typeName(T))
						}
						return fmt.Sprintf("`%s` is false: `%s` holds %s, and none of them is a `%s`", exprText(okExpr), exprText(ta.X), s.a.holdsText(ts), s.a.typeName(T))
					})
					for _, u := range used {
						w.parents = append(w.parents, datumWhys(u)...)
					}
					w.parents = append(w.parents, ts.whys()...)
					s.add(okKey, &datum{op: dEq, val: constant.MakeBool(val), why: w})
				}
			}
		}
	}
	// a container found inside a decoded value holds decoded values again
	if vk, ok := s.operand(vExpr); ok && s.a.trackable(vk.v) {
		for _, sp := range s.jsonContainerFacts(ta) {
			s.apply(vk, []spec{sp})
		}
	}
}

// assertedJSONContainer: for "y := x.([]interface{})" with a single result.
func (s *state) assertedJSONContainer(e ast.Expr) ([]spec, bool) {
	ta, ok := e.(*ast.TypeAssertExpr)
	if !ok || ta.Type == nil {
		return nil, false
	}
	out := s.jsonContainerFacts(ta)
	return out, len(out) > 0
}

// jsonContainerFacts: x.(T) where x holds decoded values and T is a
// container type: its elements are decoded values again.
func (s *state) jsonContainerFacts(ta *ast.TypeAssertExpr) []spec {
	if s.a.jsonWrite {
		return nil
	}
	T := s.a.info.TypeOf(ta.Type)
	if T == nil || !isJSONContainer(T) {
		return nil
	}
	// the interface the container is taken out of must not be handed on
	if id, isIdent := ast.Unparen(ta.X).(*ast.Ident); isIdent {
		if v, ok := s.a.info.Uses[id].(*types.Var); ok && s.a.ifaceEscapes[v] != "" {
			return nil
		}
	}
	ts, used, ok := s.valueTypes(ta.X)
	if !ok || !ts.json || !(ts.std || s.a.opts.JSONDecode) {
		return nil
	}
	set := s.jsonSetOf(ta.Pos(), key{}, ts.std)
	origin, how := oJSON, "Temporal's default JSON converter"
	if ts.std {
		origin, how = oStdJSON, "encoding/json"
	}
	d := &datum{op: dElem, ts: set, why: s.newWhy(origin, ta.Pos(), key{}, func() string {
		return "was found inside a value decoded by " + how + ", so its elements are decoded values too"
	})}
	for _, u := range used {
		d.why.parents = append(d.why.parents, datumWhys(u)...)
	}
	return []spec{{d: *d}}
}

// jsonSetOf is the type set of a decoded value: by encoding/json in this
// function (std, no assumption), or by Temporal's default JSON converter.
func (s *state) jsonSetOf(pos token.Pos, k key, std bool) *typeSet {
	origin, text := oJSON, "is decoded by Temporal's default JSON converter, so it holds only a `string`, `float64`, `bool`, `[]interface{}`, `map[string]interface{}` or nil (assumption default-json-converter)"
	if std {
		origin, text = oStdJSON, "is decoded by encoding/json (without UseNumber), so it holds only a `string`, `float64`, `bool`, `[]interface{}`, `map[string]interface{}` or nil"
	}
	w := s.newWhy(origin, pos, k, func() string { return text })
	ts := &typeSet{json: true, std: std}
	for _, t := range jsonTypes() {
		ts.add(t, w)
	}
	ts.addNil(w)
	return ts
}

// ---- decoded values: parameters and Get / Receive targets ----

// initParams gives the workflow's parameters what Temporal's decoder
// leaves in them.
func (s *state) initParams() {
	if !s.a.opts.JSONDecode {
		return
	}
	fn := s.a.g.Func()
	if fn.Type.Params == nil {
		return
	}
	for _, f := range fn.Type.Params.List {
		for _, name := range f.Names {
			v, _ := s.a.info.Defs[name].(*types.Var)
			if v == nil || !s.a.trackable(v) {
				continue
			}
			s.decoded(key{v: v}, v.Type(), name.Pos(), false)
		}
	}
}

// decoded records what a value Temporal's JSON decoder wrote into k can be.
func (s *state) decoded(k key, t types.Type, pos token.Pos, std bool) {
	switch {
	case isEmptyInterface(t):
		ts := s.jsonSetOf(pos, k, std)
		s.add(k, &datum{op: dTypes, ts: ts, why: ts.ents[0].w})
	case isJSONContainer(t) && !s.a.jsonWrite && s.a.escapes[k.v] == "":
		ts := s.jsonSetOf(pos, k, std)
		s.add(k, &datum{op: dElem, ts: ts, why: ts.ents[0].w})
	}
}

// decodeEffects: a node that calls Get / Receive with &x writes x.
func (s *state) decodeEffects(n ast.Node) {
	if len(s.a.decode) == 0 {
		return
	}
	ast.Inspect(n, func(c ast.Node) bool {
		switch x := c.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if v, ok := s.a.decode[x]; ok {
				k := key{v: v}
				// json.Unmarshal decodes into what an interface holds when it
				// holds a pointer; only a nil interface (or map/slice) is
				// replaced, so require it was nil.
				wasNil := false
				for _, d := range s.datums(k, false) {
					if d.op == dNil {
						wasNil = true
					}
				}
				s.killKey(k)
				std := s.a.decodeStd[x]
				if wasNil && (std || s.a.opts.JSONDecode) {
					s.decoded(k, v.Type(), x.Pos(), std)
				}
			}
		}
		return true
	})
}
