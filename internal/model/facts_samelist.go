package model

import (
	"go/ast"
	"go/types"
)

// extractSameList records, on range loops that walk the same list as
// another range loop, a key for the list. Only slices, arrays and strings
// are read: their length cannot change once the value exists. A map is a
// reference, so its length can change between two loops: never read. A
// range over a call result, a re-sliced list (items[1:]) or a re-assigned
// variable is a different value each time and so never equal.
func extractSameList(b *builder, g *Graph) {
	var cands []*Junction
	for _, j := range g.Junctions {
		if rs, ok := j.Stmt.(*ast.RangeStmt); ok && j.Kind == Loop && b.listLike(rs.X) {
			cands = append(cands, j)
		}
	}
	if len(cands) < 2 {
		return
	}
	fn := b.ssaFunction()
	if fn == nil {
		return
	}
	loops := b.insideLoop()
	keys := map[*Junction]string{}
	for _, j := range cands {
		rs := j.Stmt.(*ast.RangeStmt)
		if loops[rs] {
			continue
		}
		v, isAddr := fn.ValueForExpr(rs.X)
		if v == nil || isAddr {
			continue
		}
		keys[j] = "len(" + valueKey(v) + ")"
	}
	count := groupKeys(keys)
	for _, j := range cands {
		if k, ok := keys[j]; ok && count[k] >= 2 {
			j.Facts = append(j.Facts, Fact{Kind: SameList, Key: k,
				Detail: "this loop walks a list that another loop walks too, and the list cannot change in between"})
		}
	}
}

// listLike: slice, array, pointer to array or string.
func (b *builder) listLike(x ast.Expr) bool {
	t := b.info.TypeOf(x)
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Pointer:
		_, ok := u.Elem().Underlying().(*types.Array)
		return ok
	}
	return false
}
