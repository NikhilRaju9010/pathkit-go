package model

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/cfg"
)

// indexSwitches records which switch each case clause belongs to, so a
// branch go/cfg makes for a case test can be traced back to its switch.
func (b *builder) indexSwitches() {
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if body := switchBody(n); body != nil {
			for _, c := range body.List {
				b.switchOf[c.(*ast.CaseClause)] = n.(ast.Stmt)
			}
		}
		return true
	})
}

// switchBody returns the { case ... } block of a switch or type switch.
func switchBody(n ast.Node) *ast.BlockStmt {
	switch s := n.(type) {
	case *ast.SwitchStmt:
		return s.Body
	case *ast.TypeSwitchStmt:
		return s.Body
	}
	return nil
}

// switchJunction turns a whole switch into one junction (CLAUDE.md D3):
// one exit per case in source order, plus "default", written or not.
//
// go/cfg lowers a switch into a chain of tests: each test block branches
// to a case body (Succs[0]) or on to the next test (Succs[1]). first is
// the first test. The block the chain ends in is where execution goes
// when no case matches: the default's code, or the code after the switch.
func (b *builder) switchJunction(sw ast.Stmt, first *cfg.Block) Target {
	if j, ok := b.junctions[sw]; ok {
		return Target{Junction: j}
	}
	bodies := map[*ast.CaseClause]*cfg.Block{}
	blk := first
	for len(blk.Succs) == 2 {
		body := blk.Succs[0]
		cc, ok := body.Stmt.(*ast.CaseClause)
		if !ok || body.Kind != cfg.KindSwitchCaseBody || b.switchOf[cc] != sw {
			break
		}
		bodies[cc] = body
		blk = blk.Succs[1]
	}
	noMatch := blk

	j := b.newJunction(sw, Switch, switchLabel(sw))
	roads := map[*Exit]*cfg.Block{}
	written := false
	for _, c := range switchBody(sw).List {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			written = true
			roads[addExit(j, "default", cc)] = noMatch
			continue
		}
		body, ok := bodies[cc]
		if !ok {
			b.fail(fmt.Errorf("internal error: no code block for %s in %s", caseLabel(cc), b.wf.Name))
			return Target{End: endDead}
		}
		roads[addExit(j, caseLabel(cc), cc)] = body
	}
	if !written {
		roads[addExit(j, "default", nil)] = noMatch
	}
	for _, e := range j.Exits {
		e.To = b.road(roads[e])
	}
	return Target{Junction: j}
}

// addExit adds an exit to j. Labels must be unique within a junction,
// because the exit ID is "<junction>.<label>"; a repeated label gets
// " #2", " #3", ...
func addExit(j *Junction, label string, road ast.Stmt) *Exit {
	unique := label
	for n := 2; j.hasLabel(unique); n++ {
		unique = fmt.Sprintf("%s #%d", label, n)
	}
	e := &Exit{Label: unique, Junction: j, Road: road}
	j.Exits = append(j.Exits, e)
	return e
}

func (j *Junction) hasLabel(label string) bool {
	for _, e := range j.Exits {
		if e.Label == label {
			return true
		}
	}
	return false
}

// switchLabel prints a switch the way it is written: "switch status",
// "switch" (no tag), or "switch v.(type)".
func switchLabel(sw ast.Stmt) string {
	switch s := sw.(type) {
	case *ast.SwitchStmt:
		if s.Tag == nil {
			return "switch"
		}
		return "switch " + types.ExprString(s.Tag)
	case *ast.TypeSwitchStmt:
		var x ast.Expr
		switch a := s.Assign.(type) {
		case *ast.ExprStmt:
			x = a.X
		case *ast.AssignStmt:
			x = a.Rhs[0]
		}
		if ta, ok := x.(*ast.TypeAssertExpr); ok {
			return "switch " + types.ExprString(ta.X) + ".(type)"
		}
	}
	return "switch"
}

// caseLabel prints a case the way it is written: `case "complete"`,
// `case "a", "b"`, `case *MyError`.
func caseLabel(cc *ast.CaseClause) string {
	parts := make([]string, len(cc.List))
	for i, e := range cc.List {
		parts[i] = types.ExprString(e)
	}
	return "case " + strings.Join(parts, ", ")
}
