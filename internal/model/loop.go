package model

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/cfg"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
)

// loopInfo is one for or range loop, as go/cfg laid it out.
type loopInfo struct {
	stmt ast.Stmt // *ast.ForStmt or *ast.RangeStmt
	// head is where the loop decides whether to go round: the condition
	// block, the range step, or for "for {}" the body itself. It is where
	// the body's end, and every continue, come back to.
	head    *cfg.Block
	body    *cfg.Block
	done    *cfg.Block // after the loop: where "exit" and break lead
	hasExit bool       // false for "for {}": it only leaves by break or return

	// Why it is a junction (CLAUDE.md D3, as amended on 2026-09-28): a
	// Temporal call in it, or a junction in it. Neither: transparent.
	temporal, inner bool
}

func (l *loopInfo) junction() bool { return l.temporal || l.inner }

// indexLoops finds every loop's head, body and done blocks in c.
func (b *builder) indexLoops(c *cfg.CFG) {
	b.loopHeads = map[*cfg.Block]*loopInfo{}
	loops := map[ast.Stmt]*loopInfo{}
	get := func(s ast.Stmt) *loopInfo {
		if loops[s] == nil {
			loops[s] = &loopInfo{stmt: s}
		}
		return loops[s]
	}
	for _, blk := range c.Blocks {
		switch blk.Kind {
		case cfg.KindForLoop, cfg.KindRangeLoop:
			get(blk.Stmt).head = blk
		case cfg.KindForBody, cfg.KindRangeBody:
			get(blk.Stmt).body = blk
		case cfg.KindForDone, cfg.KindRangeDone:
			get(blk.Stmt).done = blk
		}
	}
	for s, l := range loops {
		l.hasExit = true
		if f, ok := s.(*ast.ForStmt); ok && f.Cond == nil {
			l.head, l.hasExit = l.body, false
		}
		l.temporal, l.inner = b.loopReasons(s)
		if l.head != nil {
			b.loopHeads[l.head] = l
		}
	}
}

// loopReasons says why a loop is a junction: it contains a call into the
// Temporal SDK (temporal), or a junction (inner): an if that counts (the
// same rules as decide) or a switch with at least one case. Code inside
// function literals is looked at for Temporal calls only, because PathKit
// doesn't map the code of closures.
func (b *builder) loopReasons(s ast.Stmt) (temporal, inner bool) {
	var parts []ast.Node
	switch s := s.(type) {
	case *ast.ForStmt:
		if s.Cond != nil {
			parts = append(parts, s.Cond)
		}
		if s.Post != nil {
			parts = append(parts, s.Post)
		}
		parts = append(parts, s.Body)
	case *ast.RangeStmt:
		parts = append(parts, s.Body) // the ranged-over value is computed once, before the loop
	}
	for _, p := range parts {
		if b.containsTemporalCall(p) {
			temporal = true
		}
		ast.Inspect(p, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.IfStmt:
				if b.ifIsJunction(n) {
					inner = true
				}
			case *ast.SwitchStmt, *ast.TypeSwitchStmt:
				for _, c := range switchBody(n).List {
					if c.(*ast.CaseClause).List != nil {
						inner = true
					}
				}
			}
			return true
		})
	}
	return temporal, inner
}

// loopRoad is where execution goes from a loop's head.
//
// A loop junction has exits "iterate" (into the body) and "exit" (to the
// code after the loop), and a retry edge: reaching the head again from
// inside the body (its end, or continue) is "retry, then back to the
// head". Paths use each of the three at most once (the analyzer half of
// the loop rule, CLAUDE.md D3).
//
// A transparent loop (no Temporal call, no junction) is walked once: into
// the body, and from its end on to the code after the loop.
func (b *builder) loopRoad(l *loopInfo) Target {
	head := l.head
	if t, ok := b.memo[head]; ok {
		return t
	}
	if b.busy[head] {
		// Back at the head from inside the body.
		if l.junction() {
			return Target{Retry: b.junctions[l.stmt].Retry}
		}
		if !l.hasExit {
			return Target{End: endDead} // "for {}" walked once can't end normally
		}
		return b.road(l.done)
	}
	b.busy[head] = true
	defer delete(b.busy, head)

	var t Target
	if !l.junction() {
		t = b.intoBody(l)
	} else {
		j := b.newJunction(l.stmt, Loop, loopLabel(l.stmt))
		iterate := addExit(j, "iterate", nil)
		var exit *Exit
		if l.hasExit {
			exit = addExit(j, "exit", nil)
		}
		j.Retry = &Exit{Label: "retry", Junction: j, To: Target{Junction: j}}
		iterate.To = b.intoBody(l)
		if exit != nil {
			exit.To = b.road(l.done)
		}
		t = Target{Junction: j}
	}
	b.memo[head] = t
	return t
}

// intoBody is the road from the start of the loop's body.
func (b *builder) intoBody(l *loopInfo) Target {
	if l.head == l.body {
		return b.computeRoad(l.body) // "for {}": the head is the body block
	}
	return b.road(l.body)
}

// loopLabel prints a loop the way it is written: "for attempt <= n",
// "for" (no condition), "for range items".
func loopLabel(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ForStmt:
		if s.Cond == nil {
			return "for"
		}
		return "for " + types.ExprString(s.Cond)
	case *ast.RangeStmt:
		return "for range " + types.ExprString(s.X)
	}
	return "for"
}

// LoopReason says why one loop is or isn't a junction.
type LoopReason struct {
	Pos token.Position
	// Temporal: the loop contains a Temporal call (a junction under the
	// original D3 rule). Junction: it contains a junction (added to the
	// rule on 2026-09-28). A loop with either is a loop junction.
	Temporal, Junction bool
}

// LoopReasons lists every loop PathKit sees in wf, in source order, with
// why it is or isn't a junction. Tests use it to show that the 2026-09-28
// change to the loop rule changes nothing for a given workflow.
func LoopReasons(wf discover.Workflow) []LoopReason {
	b := newBuilder(wf)
	var out []LoopReason
	ast.Inspect(wf.Func.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			t, j := b.loopReasons(n.(ast.Stmt))
			out = append(out, LoopReason{Pos: b.fset.Position(n.Pos()), Temporal: t, Junction: j})
		}
		return true
	})
	return out
}
