package model

import (
	"go/ast"
	"go/token"
)

// extractHistory records, for a decision on the SDK's Continue-As-New
// suggestion, which exit is "the history is getting too big":
//
//	info.GetContinueAsNewSuggested()    !info.GetContinueAsNewSuggested()
//	s := info.GetContinueAsNewSuggested(); if s { ... }
//
// (HistorySuggested), and, as a weaker note, a comparison of
// GetCurrentHistoryLength() or GetCurrentHistorySize() with a constant
// (HistorySize): the code may mean "too long", but PathKit cannot know the
// number is a real limit. A loop condition (for !suggested {...}) is not
// read.
func extractHistory(b *builder, g *Graph) {
	for _, j := range g.Junctions {
		ifs, ok := j.Stmt.(*ast.IfStmt)
		if !ok || j.Kind != PlainIf {
			continue
		}
		if exit, ok := b.suggestedExit(ifs.Cond); ok {
			j.Facts = append(j.Facts, Fact{Kind: HistorySuggested, Exit: exit, Detail: "the SDK suggests Continue-As-New because the history is large"})
		} else if exit, ok := b.historySizeExit(ifs.Cond); ok {
			j.Facts = append(j.Facts, Fact{Kind: HistorySize, Exit: exit, Detail: "the history length or size is compared with a number"})
		}
	}
}

// infoCall returns the call when e is (or is a local variable set once
// from) a call of one of the named WorkflowInfo methods.
func (b *builder) infoCall(e ast.Expr, depth int, names ...string) *ast.CallExpr {
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if isSDKMethod(b.calledFunc(e), []string{"WorkflowInfo"}, names...) {
			return e
		}
	case *ast.Ident:
		if depth >= 3 {
			return nil
		}
		if rhs, ok := b.singleDefinition(e); ok {
			return b.infoCall(rhs, depth+1, names...)
		}
	}
	return nil
}

func (b *builder) suggestedExit(cond ast.Expr) (string, bool) {
	cond = ast.Unparen(cond)
	if u, ok := cond.(*ast.UnaryExpr); ok && u.Op == token.NOT {
		if b.infoCall(u.X, 0, "GetContinueAsNewSuggested") != nil {
			return "false", true
		}
		return "", false
	}
	if b.infoCall(cond, 0, "GetContinueAsNewSuggested") != nil {
		return "true", true
	}
	return "", false
}

func (b *builder) historySizeExit(cond ast.Expr) (string, bool) {
	bin, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	op := bin.Op
	call, other := b.infoCall(bin.X, 0, "GetCurrentHistoryLength", "GetCurrentHistorySize"), bin.Y
	if call == nil {
		call, other = b.infoCall(bin.Y, 0, "GetCurrentHistoryLength", "GetCurrentHistorySize"), bin.X
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
	if call == nil {
		return "", false
	}
	if _, ok := b.intConst(other); !ok {
		return "", false
	}
	switch op {
	case token.GTR, token.GEQ:
		return "true", true
	case token.LSS, token.LEQ:
		return "false", true
	}
	return "", false
}
