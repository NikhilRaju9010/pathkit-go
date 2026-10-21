package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// FlipKind says how a decision can be forced to take one outcome (the decision
// check, milestone M11 B8).
type FlipKind string

const (
	// FlipIf: the condition of a plain if is replaced by the outcome wanted.
	FlipIf FlipKind = "if"
	// FlipError: the error variable of an `err != nil` / `err == nil` check is
	// made nil (success) or a forced failure the code can handle.
	FlipError FlipKind = "error"
)

// A Flip is the plan for forcing a decision: what to wrap and the branch IDs of
// its two outcomes. Both the recorder's markup and the decision check read it,
// so they cannot disagree.
type Flip struct {
	Kind FlipKind
	Cond ast.Expr
	// Err is the error variable (FlipError only); NotNil says the condition is
	// `err != nil` (true means failure), else `err == nil` (true means success).
	Err    *ast.Ident
	NotNil bool
	// IDTrue and IDFalse are the branch IDs of the outcomes the condition's true
	// and false take; for an error check IDFail and IDOK say which is which.
	IDTrue, IDFalse string
	IDFail, IDOK    string
}

// FlipOf returns how a decision can be forced, or "" and why it cannot. A
// decision in a summarized helper has a flip like any other: the branch IDs
// are shared by every copy of it.
func (g *Graph) FlipOf(j *Junction) (*Flip, string) {
	ifs, isIf := j.Stmt.(*ast.IfStmt)
	switch j.Kind {
	case PlainIf:
		if !isIf || ifs.Cond == nil {
			return nil, "it is not an if with a condition"
		}
		if len(j.Exits) != 2 {
			return nil, "it has no two outcomes"
		}
		f := &Flip{Kind: FlipIf, Cond: ifs.Cond}
		for _, e := range j.Exits {
			switch e.Label {
			case "true":
				f.IDTrue = g.BranchID(e)
			case "false":
				f.IDFalse = g.BranchID(e)
			}
		}
		if f.IDTrue == "" || f.IDFalse == "" {
			return nil, "its outcomes are not true and false"
		}
		return f, ""
	case ErrCheck:
		if !isIf {
			return nil, "the error check is a return of a call, which cannot be forced"
		}
		be, ok := ast.Unparen(ifs.Cond).(*ast.BinaryExpr)
		if !ok || (be.Op != token.NEQ && be.Op != token.EQL) {
			return nil, "the error check is not a plain comparison with nil"
		}
		var id *ast.Ident
		for _, side := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
			x, okx := ast.Unparen(side[0]).(*ast.Ident)
			n, okn := ast.Unparen(side[1]).(*ast.Ident)
			if okx && okn && n.Name == "nil" {
				id = x
			}
		}
		if id == nil || g.info == nil {
			return nil, "the error check is not a plain comparison of a variable with nil"
		}
		if t := g.info.TypeOf(id); t == nil || !types.Identical(t, types.Universe.Lookup("error").Type()) {
			return nil, "the checked value is not a plain error variable"
		}
		f := &Flip{Kind: FlipError, Cond: ifs.Cond, Err: id, NotNil: be.Op == token.NEQ}
		for _, e := range j.Exits {
			switch e.Label {
			case "failure":
				f.IDFail = g.BranchID(e)
			case "success":
				f.IDOK = g.BranchID(e)
			}
		}
		if f.IDFail == "" || f.IDOK == "" {
			return nil, "its outcomes are not failure and success"
		}
		return f, ""
	case Switch:
		return nil, "a switch cannot be forced yet"
	case Loop:
		return nil, "a loop cannot be forced"
	case Selector:
		return nil, "a wait on several things cannot be forced"
	}
	return nil, "this kind of decision cannot be forced"
}

// FlipReason is why the outcome of a branch cannot be forced ("" when it can).
func (g *Graph) FlipReason(b *Branch) string {
	if len(b.Exits) == 0 {
		return "no decision"
	}
	_, why := g.FlipOf(b.Exits[0].Junction)
	return why
}
