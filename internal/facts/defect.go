package facts

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// A likely defect (milestone M11). A case or comparison that no value can
// satisfy is normally impossible. But when the only reason is that the value
// is a pointer (a *string) and the code tests the pointed-to type or value
// (case string, == ""), the code almost certainly means to dereference it:
// the case is not "can't happen", it is a bug to look at. The engine then
// says so (Result.Defect), makes no claim on that path, and trusts no later
// claim on it.

// pointerHint says what to tell the person when a case for T cannot be
// taken because the set holds *T. "" when it does not apply (the set also
// holds T itself, or no pointer to T).
func (s *state) pointerHint(set *typeSet, tested types.Type) string {
	if tested == nil {
		return ""
	}
	var ptr types.Type
	for _, e := range set.ents {
		if types.Identical(e.t, tested) {
			return "" // T itself is possible: the case is not impossible for this reason
		}
		if p, ok := types.Unalias(e.t).(*types.Pointer); ok && types.Identical(p.Elem(), tested) {
			ptr = e.t
		}
	}
	if ptr == nil {
		return ""
	}
	return fmt.Sprintf("This value is a pointer (%s). Did you mean to dereference it?", s.a.typeName(ptr))
}

// noteDefect records a likely defect from the claim the engine would have
// made, and keeps the path from producing any later claim. The claim's proof
// becomes the defect's proof, with the hint added.
func (s *state) noteDefect(spec claimSpec, hint string) {
	if s.defect != nil || s.tainted {
		return
	}
	c := s.buildClaim(spec)
	s.tainted = true
	if c == nil {
		return
	}
	file, line := s.a.baseFile(spec.pos)
	proof := append([]ProofStep(nil), c.Proof...)
	if n := len(proof); n > 0 {
		proof[n-1] = ProofStep{File: file, Line: line, Text: hint}
	}
	s.defect = &Suspect{First: spec.step, Second: spec.step, Detail: hint, Explanation: c.Explanation + " " + hint, Proof: proof}
}

// ptrCompareHint looks for an interface-to-constant comparison in cond whose
// answer rests on a type set that holds *T for the constant's type T (and no T).
func (s *state) ptrCompareHint(cond ast.Expr, used []*datum) string {
	hint := ""
	ast.Inspect(cond, func(n ast.Node) bool {
		x, ok := n.(*ast.BinaryExpr)
		if !ok || hint != "" {
			return hint == ""
		}
		f, ok := s.formOf(x)
		if !ok || !f.ifaceConst {
			return true
		}
		for _, d := range used {
			if d.op == dTypes && d.ts != nil && d.why != nil && d.why.key == f.k {
				if h := s.pointerHint(d.ts, f.ctype); h != "" {
					hint = h
				}
			}
		}
		return true
	})
	return hint
}

// usesTypeSet: some fact the answer rests on is a set of types.
func usesTypeSet(used []*datum) bool {
	for _, d := range used {
		if d.op == dTypes {
			return true
		}
	}
	return false
}

// hasIfaceConst: the condition compares an interface with a constant.
func (s *state) hasIfaceConst(cond ast.Expr) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if x, ok := n.(*ast.BinaryExpr); ok {
			if f, ok := s.formOf(x); ok && f.ifaceConst {
				found = true
			}
		}
		return !found
	})
	return found
}

// constType is the type a constant compared with an interface has: its own
// type, or the default type of an untyped one.
func (s *state) constType(e ast.Expr) types.Type {
	t := s.a.info.TypeOf(e)
	if t == nil {
		return nil
	}
	if b, ok := types.Unalias(t).(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return types.Default(t)
	}
	return t
}

// ifaceConstEval answers `iface == constant` from the set of types the
// interface holds: when none of them is the constant's type, they are never
// equal.
func (s *state) ifaceConstEval(f cmpForm, shadow bool) (tri, []*datum) {
	if f.ctype == nil || (f.op != token.EQL && f.op != token.NEQ) || f.val == nil || f.val.Kind() == constant.Unknown {
		return unknown, nil
	}
	for _, d := range s.datums(f.k, shadow) {
		if d.op != dTypes || d.ts == nil {
			continue
		}
		for _, e := range d.ts.ents {
			if types.Identical(e.t, f.ctype) {
				return unknown, nil
			}
		}
		return triOf(f.op == token.NEQ), []*datum{d}
	}
	return unknown, nil
}
