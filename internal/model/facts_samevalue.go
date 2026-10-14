package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// extractSameValue records, on plain ifs that test one value that another
// plain if tests too, a key for the value and whether the if tests its
// negation. Only conditions of the form `v`, `!v`, `x > 0`, ... on values
// SSA can prove unchanged are read (see valueKey). A decision inside a loop
// is never read: another trip could see another value. Anything compound
// (a && b) is its own value and only equals an identical one.
func extractSameValue(b *builder, g *Graph) {
	var cands []*Junction
	for _, j := range g.Junctions {
		if _, ok := j.Stmt.(*ast.IfStmt); ok && j.Kind == PlainIf {
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
	neg := map[*Junction]bool{}
	for _, j := range cands {
		ifs := j.Stmt.(*ast.IfStmt)
		if loops[ifs] {
			continue
		}
		if !b.buildStable(ifs.Cond) {
			continue
		}
		e, n := ifs.Cond, false
		for {
			if p, ok := e.(*ast.ParenExpr); ok {
				e = p.X
			} else if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.NOT {
				e, n = u.X, !n
			} else {
				break
			}
		}
		v, isAddr := fn.ValueForExpr(e)
		if v == nil || isAddr {
			continue
		}
		keys[j], neg[j] = valueKey(v), n
	}
	count := groupKeys(keys)
	for _, j := range cands {
		if k, ok := keys[j]; ok && count[k] >= 2 {
			j.Facts = append(j.Facts, Fact{Kind: SameValue, Key: k, Negated: neg[j],
				Detail: "this decision tests a value that another decision tests too, and it cannot change in between"})
		}
	}
}

// buildStable reports whether the condition reads nothing whose value
// depends on the machine or the build: no name from another package
// (strconv.IntSize, math.MaxInt, runtime.GOOS, unsafe.Sizeof), no constant
// declared in a file with a build constraint or defined from such a thing,
// no array type of a machine-dependent size. Two different constants that
// happen to be equal on this machine would otherwise look like one value.
func (b *builder) buildStable(cond ast.Expr) bool {
	ok := true
	seen := map[types.Object]bool{}
	ast.Inspect(cond, func(n ast.Node) bool {
		id, isID := n.(*ast.Ident)
		if !isID || !ok {
			return ok
		}
		switch obj := b.info.Uses[id].(type) {
		case *types.PkgName:
			ok = false
		case *types.Const:
			if obj.Pkg() != nil && (obj.Pkg() != b.wf.Pkg.Types || !b.trustedConstDecl(obj, seen)) {
				ok = false
			}
		case *types.Builtin:
			if obj.Pkg() != nil {
				ok = false
			}
		case *types.TypeName:
			if _, isArray := obj.Type().Underlying().(*types.Array); isArray {
				ok = false
			}
		}
		return ok
	})
	return ok
}
