package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/cfg"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
)

// UnsupportedError means the workflow uses a construct PathKit can't map.
// The workflow is skipped rather than shown with a half-right map.
type UnsupportedError struct {
	Construct string // e.g. "goto"
	Pos       token.Position
	Reason    string // why PathKit doesn't map it
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s at %s:%d is not supported: %s", e.Construct, filepath.Base(e.Pos.Filename), e.Pos.Line, e.Reason)
}

type builder struct {
	wf        discover.Workflow
	info      *types.Info
	fset      *token.FileSet
	pragmas   map[int]string // line -> "ignore" / "branch"
	memo      map[*cfg.Block]Target
	busy      map[*cfg.Block]bool
	junctions map[ast.Node]*Junction
	switchOf  map[*ast.CaseClause]ast.Stmt // case clause -> its switch
	loopHeads map[*cfg.Block]*loopInfo     // a loop's head block -> the loop
	selectAt  map[ast.Stmt]*selectorInfo   // a checked Select statement -> its selector
	// contOf marks the blocks of a selector callback: a return in the
	// callback, or its end, goes on to the code after the Select call.
	contOf map[*cfg.Block]func() Target
	memoAt map[blockPos]Target // roads that start in the middle of a block
	err    error
}

// blockPos is a place inside a block: before its node i.
type blockPos struct {
	blk *cfg.Block
	i   int
}

// Build makes the junction map of one workflow.
func Build(wf discover.Workflow) (*Graph, error) {
	b := newBuilder(wf)
	if u := b.findUnsupported(); u != nil {
		return nil, u
	}

	c := cfg.New(wf.Func.Body, b.mayReturn)
	b.indexLoops(c)
	start := b.road(c.Blocks[0])
	if b.err != nil {
		return nil, b.err
	}
	return b.finish(start), nil
}

func newBuilder(wf discover.Workflow) *builder {
	b := &builder{
		wf:        wf,
		info:      wf.Pkg.TypesInfo,
		fset:      wf.Pkg.Fset,
		memo:      map[*cfg.Block]Target{},
		busy:      map[*cfg.Block]bool{},
		junctions: map[ast.Node]*Junction{},
		switchOf:  map[*ast.CaseClause]ast.Stmt{},
		loopHeads: map[*cfg.Block]*loopInfo{},
		selectAt:  map[ast.Stmt]*selectorInfo{},
		contOf:    map[*cfg.Block]func() Target{},
		memoAt:    map[blockPos]Target{},
	}
	b.pragmas = b.readPragmas()
	b.indexSwitches()
	return b
}

// finish numbers the junctions in source order and assigns every ID.
func (b *builder) finish(start Target) *Graph {
	g := &Graph{Workflow: b.wf.Name, Start: start, byStmt: b.junctions, byID: map[string]*Exit{}}
	for _, j := range b.junctions {
		g.Junctions = append(g.Junctions, j)
	}
	sort.Slice(g.Junctions, func(i, k int) bool { return g.Junctions[i].order < g.Junctions[k].order })
	for i, j := range g.Junctions {
		j.ID = fmt.Sprintf("J%d", i+1)
		exits := j.Exits
		if j.Retry != nil {
			exits = append(exits[:len(exits):len(exits)], j.Retry)
		}
		for _, e := range exits {
			e.ID = EdgeID{j.ID + "." + e.Label}
			g.byID[e.ID.s] = e
		}
	}
	return g
}

// road returns where execution goes from the start of blk.
func (b *builder) road(blk *cfg.Block) Target {
	if t, ok := b.memo[blk]; ok {
		return t
	}
	if l := b.loopHeads[blk]; l != nil {
		return b.loopRoad(l)
	}
	if b.busy[blk] {
		// Only loops or goto can make a cycle; loop heads are handled
		// above and goto is rejected before building. Reaching here is a
		// bug.
		b.fail(fmt.Errorf("internal error: unexpected cycle in %s", b.wf.Name))
		return Target{End: endDead}
	}
	b.busy[blk] = true
	t := b.computeRoad(blk)
	delete(b.busy, blk)
	b.memo[blk] = t
	return t
}

// roadFrom returns where execution goes from before node i of blk.
func (b *builder) roadFrom(blk *cfg.Block, i int) Target {
	if i == 0 {
		return b.road(blk)
	}
	at := blockPos{blk, i}
	if t, ok := b.memoAt[at]; ok {
		return t
	}
	t := b.computeRoadFrom(blk, i)
	b.memoAt[at] = t
	return t
}

func (b *builder) computeRoad(blk *cfg.Block) Target { return b.computeRoadFrom(blk, 0) }

// computeRoadFrom is where execution goes from before node from of blk.
// A road that passes a saga compensation defer carries the note (CLAUDE.md
// D3: noted, never a branch).
func (b *builder) computeRoadFrom(blk *cfg.Block, from int) Target {
	t, passed := b.roadThrough(blk, from)
	if passed {
		t.Compensation = true
	}
	return t
}

// roadThrough does the work of computeRoadFrom; passed reports whether
// the nodes it went through register a compensation defer.
func (b *builder) roadThrough(blk *cfg.Block, from int) (t Target, passed bool) {
	cont := b.contOf[blk] // set inside a selector callback
	for i, n := range blk.Nodes[from:] {
		// A defer in a selector callback runs when the callback ends, not
		// when the workflow does: only the workflow's own defers count.
		if d, ok := n.(*ast.DeferStmt); ok && cont == nil && b.isCompensation(d) {
			passed = true
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			if cont != nil {
				return cont(), passed // returns from the callback, not the workflow
			}
			return Target{End: b.endKind(ret)}, passed
		}
		if s, ok := n.(ast.Stmt); ok && b.selectAt[s] != nil {
			return b.selectorJunction(b.selectAt[s], blk, from+i), passed
		}
	}
	switch len(blk.Succs) {
	case 0:
		if cont != nil && !b.endsInNoReturn(blk) {
			return cont(), passed // the callback's end
		}
		return Target{End: endDead}, passed // a panic, os.Exit, ...
	case 1:
		return b.road(blk.Succs[0]), passed
	}
	then, other := blk.Succs[0], blk.Succs[1]
	if ifs, ok := then.Stmt.(*ast.IfStmt); ok && then.Kind == cfg.KindIfThen {
		return b.decide(ifs, then, other), passed
	}
	if clause, ok := then.Stmt.(*ast.CaseClause); ok && then.Kind == cfg.KindSwitchCaseBody {
		if sw := b.switchOf[clause]; sw != nil {
			return b.switchJunction(sw, blk), passed
		}
	}
	b.fail(fmt.Errorf("internal error: unexpected branch (%s) in %s", then.Kind, b.wf.Name))
	return Target{End: endDead}, passed
}

// decide turns one if statement into a junction, or walks straight
// through it when it is transparent or ignored (CLAUDE.md D2).
func (b *builder) decide(ifs *ast.IfStmt, then, other *cfg.Block) Target {
	pragma := b.ifPragma(ifs)
	if obj, neq, isErrForm := b.errForm(ifs.Cond); isErrForm {
		label, temporal := b.errSource(obj, ifs)
		noErrorIsThen := !neq
		if pragma == "ignore" || (!temporal && pragma != "branch") {
			return b.transparent(noErrorIsThen, then, other)
		}
		j := b.newJunction(ifs, ErrCheck, label)
		failure, success := then, other
		failureRoad, successRoad := ast.Stmt(ifs.Body), ifs.Else
		if !neq {
			failure, success = other, then
			failureRoad, successRoad = ifs.Else, ifs.Body
		}
		j.Exits = []*Exit{
			{Label: "failure", Junction: j, Road: failureRoad},
			{Label: "success", Junction: j, Road: successRoad},
		}
		j.Exits[0].To = b.road(failure)
		j.Exits[1].To = b.road(success)
		return Target{Junction: j}
	}

	if pragma == "ignore" {
		return b.transparent(false, then, other) // assume a defensive check doesn't fire
	}
	if name, arrivedIsTrue, ok := b.waitResult(ifs.Cond); ok {
		arrived, missed := "received", "not received"
		if name == "AwaitWithTimeout" {
			arrived, missed = "signaled", "timeout"
		}
		j := b.newJunction(ifs, WaitResult, "if "+types.ExprString(ifs.Cond)+" ("+name+")")
		arrivedBlk, missedBlk := then, other
		arrivedRoad, missedRoad := ast.Stmt(ifs.Body), ifs.Else
		if !arrivedIsTrue {
			arrivedBlk, missedBlk = other, then
			arrivedRoad, missedRoad = ifs.Else, ifs.Body
		}
		j.Exits = []*Exit{
			{Label: arrived, Junction: j, Road: arrivedRoad},
			{Label: missed, Junction: j, Road: missedRoad},
		}
		j.Exits[0].To = b.road(arrivedBlk)
		j.Exits[1].To = b.road(missedBlk)
		return Target{Junction: j}
	}
	j := b.newJunction(ifs, PlainIf, "if "+types.ExprString(ifs.Cond))
	j.Exits = []*Exit{
		{Label: "true", Junction: j, Road: ifs.Body},
		{Label: "false", Junction: j, Road: ifs.Else},
	}
	j.Exits[0].To = b.road(then)
	j.Exits[1].To = b.road(other)
	return Target{Junction: j}
}

// ifPragma returns "ignore" or "branch" when that pragma is on the if's
// own line or the line above, else "".
func (b *builder) ifPragma(ifs *ast.IfStmt) string {
	if p := b.pragmas[b.line(ifs)]; p != "" {
		return p
	}
	return b.pragmas[b.line(ifs)-1]
}

// ifIsJunction reports whether decide makes ifs a junction, without
// building anything. It must follow exactly the same rules as decide.
func (b *builder) ifIsJunction(ifs *ast.IfStmt) bool {
	pragma := b.ifPragma(ifs)
	if pragma == "ignore" {
		return false
	}
	if obj, _, isErrForm := b.errForm(ifs.Cond); isErrForm {
		_, temporal := b.errSource(obj, ifs)
		return temporal || pragma == "branch"
	}
	return true
}

func (b *builder) transparent(takeThen bool, then, other *cfg.Block) Target {
	if takeThen {
		return b.road(then)
	}
	return b.road(other)
}

func (b *builder) newJunction(stmt ast.Node, kind JunctionKind, label string) *Junction {
	j := &Junction{Kind: kind, Label: label, Stmt: stmt, Pos: b.fset.Position(stmt.Pos()), order: stmt.Pos()}
	b.junctions[stmt] = j
	return j
}

// errForm reports whether cond is exactly "v != nil", "v == nil",
// "nil != v" or "nil == v" for an error variable v.
func (b *builder) errForm(cond ast.Expr) (obj types.Object, neq, ok bool) {
	bin, isBin := ast.Unparen(cond).(*ast.BinaryExpr)
	if !isBin || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return nil, false, false
	}
	x, y := ast.Unparen(bin.X), ast.Unparen(bin.Y)
	if b.isNil(x) {
		x, y = y, x
	}
	id, isIdent := x.(*ast.Ident)
	if !isIdent || !b.isNil(y) {
		return nil, false, false
	}
	obj = b.info.Uses[id]
	if v, isVar := obj.(*types.Var); !isVar || !types.Identical(v.Type(), errorType) {
		return nil, false, false
	}
	return obj, bin.Op == token.NEQ, true
}

func (b *builder) isNil(e ast.Expr) bool {
	tv, ok := b.info.Types[e]
	return ok && tv.IsNil()
}

// nearestAssign finds the most recent assignment to obj before pos in the
// workflow function (not inside function literals). idx is obj's position
// in a multi-value assignment like "ok, err := f()".
func (b *builder) nearestAssign(obj types.Object, pos token.Pos) (rhs ast.Expr, idx int, found bool) {
	best := token.NoPos
	consider := func(lhs []ast.Expr, values []ast.Expr, at token.Pos) {
		for i, l := range lhs {
			id, ok := ast.Unparen(l).(*ast.Ident)
			if !ok || at >= pos || at < best {
				continue
			}
			if b.info.Defs[id] != obj && b.info.Uses[id] != obj {
				continue
			}
			switch {
			case len(values) == len(lhs):
				rhs, idx = values[i], 0
			case len(values) == 1:
				rhs, idx = values[0], i
			default:
				continue
			}
			best, found = at, true
		}
	}
	// Look in the function pos is in first (a selector callback, say),
	// then in the functions around it, out to the workflow function.
	for _, body := range b.bodiesAround(pos) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.AssignStmt:
				consider(n.Lhs, n.Rhs, n.Pos())
			case *ast.ValueSpec:
				lhs := make([]ast.Expr, len(n.Names))
				for i, name := range n.Names {
					lhs[i] = name
				}
				consider(lhs, n.Values, n.Pos())
			}
			return true
		})
		if found {
			break
		}
	}
	return rhs, idx, found
}

// bodiesAround lists the bodies of the functions pos is inside: the
// innermost function literal first, the workflow function last.
func (b *builder) bodiesAround(pos token.Pos) []*ast.BlockStmt {
	out := []*ast.BlockStmt{b.wf.Func.Body}
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos >= n.End() {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			out = append([]*ast.BlockStmt{lit.Body}, out...)
		}
		return true
	})
	return out
}

// endKind works out how a return statement ends the workflow.
func (b *builder) endKind(ret *ast.ReturnStmt) EndKind {
	if len(ret.Results) == 0 {
		return EndUnknown // bare return with named results
	}
	last := ast.Unparen(ret.Results[len(ret.Results)-1])
	if b.isNil(last) {
		return EndCompleted
	}
	switch e := last.(type) {
	case *ast.CallExpr:
		f := b.calledFunc(e)
		switch {
		case isPkgFunc(f, workflowPkg, "NewContinueAsNewError"):
			return EndContinuedAsNew
		case isPkgFunc(f, "fmt", "Errorf"), isPkgFunc(f, "errors", "New"):
			return EndFailed
		case isPkgFunc(f, temporalPkg) && strings.HasPrefix(f.Name(), "New") && strings.HasSuffix(f.Name(), "Error"):
			return EndFailed
		}
	case *ast.Ident:
		if b.knownNonNil(b.info.Uses[e], ret) {
			return EndFailed
		}
	}
	return EndUnknown
}

// knownNonNil reports whether ret sits on the "error" side of an if that
// checks obj against nil.
func (b *builder) knownNonNil(obj types.Object, ret *ast.ReturnStmt) bool {
	path, _ := astutil.PathEnclosingInterval(b.wf.File, ret.Pos(), ret.End())
	for i := 1; i < len(path); i++ {
		ifs, ok := path[i].(*ast.IfStmt)
		if !ok {
			continue
		}
		checked, neq, isErrForm := b.errForm(ifs.Cond)
		if !isErrForm || checked != obj {
			continue
		}
		inBody := path[i-1] == ast.Node(ifs.Body)
		inElse := ifs.Else != nil && path[i-1] == ast.Node(ifs.Else)
		if (inBody && neq) || (inElse && !neq) {
			return true
		}
	}
	return false
}

// endsInNoReturn reports a block cut short by a call that never returns
// (panic, os.Exit, ...).
func (b *builder) endsInNoReturn(blk *cfg.Block) bool {
	if len(blk.Nodes) == 0 {
		return false
	}
	es, ok := blk.Nodes[len(blk.Nodes)-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	return ok && !b.mayReturn(call)
}

// mayReturn tells go/cfg which calls never return (panic, os.Exit,
// log.Fatal...), so paths through them are dropped.
func (b *builder) mayReturn(call *ast.CallExpr) bool {
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if bi, ok := b.info.Uses[id].(*types.Builtin); ok && bi.Name() == "panic" {
			return false
		}
	}
	f := b.calledFunc(call)
	return !isPkgFunc(f, "os", "Exit") && !isPkgFunc(f, "log", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln")
}

func (b *builder) readPragmas() map[int]string {
	m := map[int]string{}
	for _, cg := range b.wf.File.Comments {
		for _, c := range cg.List {
			line := b.fset.Position(c.Slash).Line
			switch {
			case strings.HasPrefix(c.Text, "//pathkit:ignore"):
				m[line] = "ignore"
			case strings.HasPrefix(c.Text, "//pathkit:branch"):
				m[line] = "branch"
			}
		}
	}
	return m
}

func (b *builder) line(n ast.Node) int { return b.fset.Position(n.Pos()).Line }

func (b *builder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}
