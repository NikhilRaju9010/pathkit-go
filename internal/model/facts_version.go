package model

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
)

// extractVersion records, for a decision on the result of
// workflow.GetVersion, which exit is the OLD side (the code kept so old
// histories still replay). Only exact shapes are read; anything else
// records nothing, which is the safe side:
//
//	v == workflow.DefaultVersion      (old = true)     v != ... (old = false)
//	v < N, v <= N                     (old = true)     v > N, v >= N (old = false)
//	switch v { case <old versions> }  (each such case)
//
// where v is the call itself or a local variable set once from it, N is a
// constant, and the call's min and max versions are constants too. The
// check must really split the versions that can occur (min..max): a side
// that can never happen is not an "old branch", so nothing is recorded.
func extractVersion(b *builder, g *Graph) {
	for _, j := range g.Junctions {
		switch s := j.Stmt.(type) {
		case *ast.IfStmt:
			if j.Kind != PlainIf {
				continue
			}
			if f, ok := b.versionCondition(s.Cond); ok {
				j.Facts = append(j.Facts, f)
			}
		case *ast.SwitchStmt:
			if j.Kind != Switch || s.Tag == nil {
				continue
			}
			call := b.versionCall(s.Tag, 0)
			if call == nil {
				continue
			}
			id, lo, hi, ok := b.versionRange(call)
			if !ok {
				continue
			}
			for _, e := range j.Exits {
				cc, isCase := e.Road.(*ast.CaseClause)
				if !isCase || cc.List == nil || !b.allBelow(cc.List, lo, hi) {
					continue
				}
				j.Facts = append(j.Facts, Fact{Kind: VersionCheck, Exit: e.Label, Name: id,
					Detail: fmt.Sprintf("GetVersion(%q) returns a version older than %d", id, hi)})
			}
		}
	}
}

// allBelow reports whether every expression is a constant in [lo, hi).
func (b *builder) allBelow(list []ast.Expr, lo, hi int64) bool {
	for _, e := range list {
		v, ok := b.intConst(e)
		if !ok || v < lo || v >= hi {
			return false
		}
	}
	return true
}

func (b *builder) intConst(e ast.Expr) (int64, bool) {
	tv, ok := b.info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(tv.Value)
}

// versionCall returns the workflow.GetVersion call an expression is, or a
// local variable set once from.
func (b *builder) versionCall(e ast.Expr, depth int) *ast.CallExpr {
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if isPkgFunc(b.calledFunc(e), workflowPkg, "GetVersion") && len(e.Args) == 4 {
			return e
		}
	case *ast.Ident:
		if depth >= 3 {
			return nil
		}
		if rhs, ok := b.singleDefinition(e); ok {
			return b.versionCall(rhs, depth+1)
		}
	}
	return nil
}

// versionRange reads the change id and the min and max versions of a
// GetVersion call; all three must be constants.
func (b *builder) versionRange(call *ast.CallExpr) (id string, lo, hi int64, ok bool) {
	tv, has := b.info.Types[call.Args[1]]
	if !has || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", 0, 0, false
	}
	lo, ok1 := b.intConst(call.Args[2])
	hi, ok2 := b.intConst(call.Args[3])
	if !ok1 || !ok2 || lo > hi {
		return "", 0, 0, false
	}
	return constant.StringVal(tv.Value), lo, hi, true
}

// versionCondition reads an if condition on a GetVersion result. Besides the
// comparison itself it follows a bool variable set once from one, a negation
// of one, and a call of a function of the package whose only statement is
// "return <such a comparison>" (a version gate written as a helper).
func (b *builder) versionCondition(cond ast.Expr) (Fact, bool) {
	return b.versionConditionAt(cond, 0)
}

func (b *builder) versionConditionAt(cond ast.Expr, depth int) (Fact, bool) {
	if depth > 3 {
		return Fact{}, false
	}
	switch e := ast.Unparen(cond).(type) {
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return Fact{}, false
		}
		f, ok := b.versionConditionAt(e.X, depth+1)
		if !ok {
			return Fact{}, false
		}
		if f.Exit == "true" {
			f.Exit = "false"
		} else {
			f.Exit = "true"
		}
		return f, true
	case *ast.Ident:
		if rhs, ok := b.singleDefinition(e); ok {
			return b.versionConditionAt(rhs, depth+1)
		}
		return Fact{}, false
	case *ast.CallExpr:
		h := b.calleeOf(e)
		if h == nil || h.body == nil || len(h.body.List) != 1 {
			return Fact{}, false
		}
		ret, ok := h.body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return Fact{}, false
		}
		return b.versionConditionAt(ret.Results[0], depth+1)
	}
	return b.versionComparison(cond)
}

func (b *builder) versionComparison(cond ast.Expr) (Fact, bool) {
	bin, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return Fact{}, false
	}
	op := bin.Op
	x, y := bin.X, bin.Y
	call := b.versionCall(x, 0)
	n, isConst := b.intConst(y)
	if call == nil || !isConst {
		// N on the left: flip the comparison.
		call, n, isConst = b.versionCall(y, 0), 0, false
		if call == nil {
			return Fact{}, false
		}
		n, isConst = b.intConst(x)
		if !isConst {
			return Fact{}, false
		}
		switch op {
		case token.LSS:
			op = token.GTR
		case token.LEQ:
			op = token.GEQ
		case token.GTR:
			op = token.LSS
		case token.GEQ:
			op = token.LEQ
		}
	}
	id, lo, hi, ok := b.versionRange(call)
	if !ok {
		return Fact{}, false
	}
	// Which versions (lo..hi) make the condition true?
	holds := func(v int64) bool {
		switch op {
		case token.EQL:
			return v == n
		case token.NEQ:
			return v != n
		case token.LSS:
			return v < n
		case token.LEQ:
			return v <= n
		case token.GTR:
			return v > n
		case token.GEQ:
			return v >= n
		}
		return false
	}
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
	default:
		return Fact{}, false
	}
	// Both sides must be possible, or the check is not a real gate.
	trueSide, falseSide := false, false
	if uint64(hi-lo) >= 1000 {
		return Fact{}, false // too many versions to check
	}
	for v := lo; v <= hi; v++ {
		if holds(v) {
			trueSide = true
		} else {
			falseSide = true
		}
	}
	if !trueSide || !falseSide {
		return Fact{}, false
	}
	// The old side is the one that contains the oldest version (lo). With
	// == / != only the exact "is it the oldest version" test is an old
	// branch (the usual DefaultVersion check).
	if (op == token.EQL || op == token.NEQ) && n != lo {
		return Fact{}, false
	}
	exit := "false"
	if holds(lo) {
		exit = "true"
	}
	return Fact{Kind: VersionCheck, Exit: exit, Name: id,
		Detail: fmt.Sprintf("GetVersion(%q) returns an older version here", id)}, true
}
