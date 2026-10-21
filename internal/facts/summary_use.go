package facts

import (
	"fmt"
	"go/ast"
	"go/types"
)

// summarySpecs gives the facts a summary of the called function holds about
// its idx-th result, for a variable of type lt (nil when nothing is known).
func (s *state) summarySpecs(call *ast.CallExpr, idx int, lt types.Type) []spec {
	sum := s.a.summaryOf(call, 0)
	if sum == nil || idx >= len(sum.results) || sum.results[idx] == nil {
		return nil
	}
	r := sum.results[idx]
	name := sum.fn.Name()
	where := fmt.Sprintf("`%s` (%s:%d)", name, sum.file, sum.line)
	var out []spec
	switch {
	case isInterface(lt) && r.typesKnown:
		ts := &typeSet{}
		w := s.newWhy(oStore, call.Pos(), key{}, func() string {
			return fmt.Sprintf("is returned by %s, whose return statements give %s", where, s.a.holdsText(typeSetOf(r.types, r.mayNil)))
		})
		for _, t := range r.types {
			ts.add(t, w)
		}
		if r.mayNil {
			ts.addNil(w)
		}
		out = append(out, spec{d: *s.typesDatum(ts, call.Pos(), key{})})
	case isMap(lt) && r.mapKnown:
		union := &typeSet{}
		perKey := map[string]*typeSet{}
		for _, k := range r.order {
			ent := r.perKey[k]
			if !ent.known {
				return nil // some value stored there has a type nobody can tell
			}
			k, ent := k, ent
			w := s.newWhy(oStore, call.Pos(), key{}, func() string {
				return fmt.Sprintf("is a map built by %s, which stores %s under %q", where, s.a.holdsText(typeSetOf(ent.types, ent.mayNil)), k)
			})
			ks := &typeSet{}
			for _, t := range ent.types {
				ks.add(t, w)
				union.add(t, w)
			}
			if ent.mayNil {
				ks.addNil(w)
				union.addNil(w)
			}
			perKey[k] = ks
		}
		w := s.newWhy(oStore, call.Pos(), key{}, func() string { return fmt.Sprintf("is a map built by %s", where) })
		out = append(out, spec{d: datum{op: dElem, ts: union, perKey: perKey, why: w}})
	}
	if r.cnst != nil && (kindOf(lt) == tkBool || kindOf(lt) == tkString || kindOf(lt) == tkNumber) && types.Identical(types.Default(lt), lt) {
		c := r.cnst
		w := s.newWhy(oConst, call.Pos(), key{}, func() string {
			return fmt.Sprintf("is returned by %s, which gives %s on every return", where, c.ExactString())
		})
		out = append(out, spec{d: datum{op: dEq, val: c, why: w}})
	}
	if !isInterface(lt) && kindOf(lt) == tkNilable {
		switch {
		case r.alwaysNil:
			out = append(out, spec{d: datum{op: dNil, why: s.newWhy(oNil, call.Pos(), key{}, func() string { return fmt.Sprintf("is returned by %s, which gives nil on every return", where) })}})
		case r.neverNil:
			out = append(out, spec{d: datum{op: dNonNil, why: s.newWhy(oNonNil, call.Pos(), key{}, func() string { return fmt.Sprintf("is returned by %s, which never gives nil", where) })}})
		}
	}
	return out
}

// typeSetOf is a plain list of types as a set (for the wording of a sentence).
func typeSetOf(ts []types.Type, mayNil bool) *typeSet {
	set := &typeSet{mayNil: mayNil}
	for _, t := range ts {
		set.ents = append(set.ents, typeEntry{t: t})
	}
	return set
}
