package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

// ConditionIDPrefix starts every condition ID ("c:" and 12 hex digits).
const ConditionIDPrefix = "c:"

// A Condition is one atomic part of a decision's condition: in
// `if a && (b || c)` the parts are `a`, `b` and `c`. Only a decision with two
// or more parts has any (a single part is the decision itself, already
// counted as a branch). The recorder notes, per part, whether it was ever
// true and ever false when it was evaluated; `&&` and `||` skip the parts
// after the one that settles the answer, so a part is noted only on the runs
// that got to it.
type Condition struct {
	ID string
	// Decision is the junction the part is in (its first copy for a helper's).
	Decision *Junction
	// Text is the part as written, whitespace collapsed.
	Text string
	// File, Line and Func say where, as the branch IDs do.
	File string
	Line int
	Func string
	// Expr is the part in the code.
	Expr ast.Expr
}

// conditionOf returns the condition expression of a decision that has one:
// an if's, or a for loop's.
func conditionOf(j *Junction) ast.Expr {
	switch s := j.Stmt.(type) {
	case *ast.IfStmt:
		if j.Kind == PlainIf {
			return s.Cond
		}
	case *ast.ForStmt:
		if j.Kind == Loop {
			return s.Cond
		}
	}
	return nil
}

// leaves splits a condition at && and || (looking through parentheses).
func leaves(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return leaves(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			return append(leaves(x.X), leaves(x.Y)...)
		}
	}
	return []ast.Expr{e}
}

// Conditions lists the atomic parts of every decision whose condition is an
// `&&` / `||` chain, once per place in the source (a helper copied in several
// times has one set), in source order. The IDs come only from here.
func (g *Graph) Conditions() []*Condition {
	if g.conditionsOK {
		return g.conditions
	}
	g.Branches() // numbers the decisions
	g.conditionsOK = true
	g.condsByStmt = map[ast.Node][]*Condition{}
	done := map[ast.Node]bool{}
	for _, j := range g.AllJunctions() {
		cond := conditionOf(j)
		if cond == nil || done[j.Stmt] {
			continue
		}
		parts := leaves(cond)
		if len(parts) < 2 {
			continue
		}
		done[j.Stmt] = true
		d := g.declOf[j]
		seen := map[string]int{}
		for _, p := range parts {
			text := g.exprText(p)
			seen[text]++
			sum := sha256.Sum256([]byte("c1\n" + d.file + "\n" + d.fn + "\n" + d.words + "\n" + strconv.Itoa(g.occOf[j]) + "\n" + text + "\n" + strconv.Itoa(seen[text])))
			c := &Condition{ID: ConditionIDPrefix + hex.EncodeToString(sum[:])[:12], Decision: j, Text: text, File: d.file, Line: g.fset.Position(p.Pos()).Line, Func: d.fn, Expr: p}
			g.conditions = append(g.conditions, c)
			g.condsByStmt[j.Stmt] = append(g.condsByStmt[j.Stmt], c)
		}
	}
	return g.conditions
}

// ConditionsOf lists the parts of one decision (nil when it has none).
func (g *Graph) ConditionsOf(j *Junction) []*Condition {
	g.Conditions()
	return g.condsByStmt[j.Stmt]
}

func (g *Graph) exprText(e ast.Expr) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, g.fset, e); err != nil {
		return "?"
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
