package facts

import (
	"go/types"
)

// typeEntry is one dynamic type an interface value may hold, and the
// statement that put it there.
type typeEntry struct {
	t types.Type
	w *why
}

// typeSet is what is known about the dynamic types of interface values: the
// values of one interface variable, or the elements of a slice or map that
// was built in this function. It is a closed list: a type not in it is
// impossible. mayNil says that a nil interface is possible too.
type typeSet struct {
	ents   []typeEntry
	mayNil bool
	nilWhy *why
	// json: the values come from Temporal's default JSON converter, so the
	// list is the fixed one for decoded values (see jsonTypes). Containers
	// found inside such a value hold the same list again.
	json bool
}

func (ts *typeSet) clone() *typeSet {
	c := *ts
	c.ents = append([]typeEntry(nil), ts.ents...)
	return &c
}

func (ts *typeSet) has(t types.Type) bool {
	for _, e := range ts.ents {
		if types.Identical(e.t, t) {
			return true
		}
	}
	return false
}

func (ts *typeSet) add(t types.Type, w *why) {
	if !ts.has(t) {
		ts.ents = append(ts.ents, typeEntry{t, w})
	}
}

func (ts *typeSet) addNil(w *why) {
	if !ts.mayNil {
		ts.mayNil, ts.nilWhy = true, w
	}
}

// union adds everything in o to ts.
func (ts *typeSet) union(o *typeSet) {
	for _, e := range o.ents {
		ts.add(e.t, e.w)
	}
	if o.mayNil {
		ts.addNil(o.nilWhy)
	}
	// json stays false unless both are decoded values; a fresh set starts false.
	ts.json = ts.json && o.json
}

// whys lists the reasons behind every entry (and the nil, if any).
func (ts *typeSet) whys() []*why {
	var out []*why
	for _, e := range ts.ents {
		out = append(out, e.w)
	}
	if ts.mayNil && ts.nilWhy != nil {
		out = append(out, ts.nilWhy)
	}
	return out
}

// jsonTypes are the only dynamic types Temporal's default JSON converter
// puts into an interface{} (docs/DESIGN.md D14, assumption default-json-converter).
func jsonTypes() []types.Type {
	empty := types.NewInterfaceType(nil, nil)
	return []types.Type{
		types.Typ[types.String], types.Typ[types.Float64], types.Typ[types.Bool],
		types.NewSlice(empty), types.NewMap(types.Typ[types.String], empty),
	}
}

func isEmptyInterface(t types.Type) bool {
	i, ok := t.Underlying().(*types.Interface)
	return ok && i.NumMethods() == 0 && !isTypeParam(t)
}

func isTypeParam(t types.Type) bool {
	_, ok := t.(*types.TypeParam)
	return ok
}

// isInterface reports an interface type that is not a type parameter.
func isInterface(t types.Type) bool {
	if t == nil || isTypeParam(t) {
		return false
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// concrete reports a type whose values are exactly that type at run time:
// not an interface, not a type parameter, not the type of nil.
func concrete(t types.Type) bool {
	if t == nil || isTypeParam(t) || isInterface(t) {
		return false
	}
	if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return false
	}
	return true
}

// elemOf returns the element type of a slice or map type, and whether it is one.
func elemOf(t types.Type) (types.Type, bool) {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return u.Elem(), true
	case *types.Map:
		return u.Elem(), true
	}
	return nil, false
}

func isMap(t types.Type) bool {
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// matchesCase: does a value of dynamic type t take a "case caseType" of a
// type switch (or satisfy x.(caseType))?
func matchesCase(t, caseType types.Type) bool {
	if isInterface(caseType) {
		return types.Implements(t, caseType.Underlying().(*types.Interface))
	}
	return types.Identical(t, caseType)
}
