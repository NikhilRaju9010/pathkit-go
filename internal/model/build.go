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

// UnsupportedError means the workflow uses a construct PathKit can't map
// yet. The workflow is skipped rather than shown with a half-right map.
type UnsupportedError struct {
	Construct string // e.g. "for loop"
	Pos       token.Position
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s at %s:%d is supported from M4", e.Construct, filepath.Base(e.Pos.Filename), e.Pos.Line)
}

type builder struct {
	wf        discover.Workflow
	info      *types.Info
	fset      *token.FileSet
	pragmas   map[int]string // line -> "ignore" / "branch"
	memo      map[*cfg.Block]Target
	busy      map[*cfg.Block]bool
	junctions map[*ast.IfStmt]*Junction
	err       error
}

// Build makes the junction map of one workflow.
func Build(wf discover.Workflow) (*Graph, error) {
	b := &builder{
		wf:        wf,
		info:      wf.Pkg.TypesInfo,
		fset:      wf.Pkg.Fset,
		memo:      map[*cfg.Block]Target{},
		busy:      map[*cfg.Block]bool{},
		junctions: map[*ast.IfStmt]*Junction{},
	}
	b.pragmas = b.readPragmas()
	if u := b.findUnsupported(); u != nil {
		return nil, u
	}

	c := cfg.New(wf.Func.Body, b.mayReturn)
	start := b.road(c.Blocks[0])
	if b.err != nil {
		return nil, b.err
	}
	return b.finish(start), nil
}

// finish numbers the junctions in source order and assigns every ID.
func (b *builder) finish(start Target) *Graph {
	g := &Graph{Workflow: b.wf.Name, Start: start, byStmt: b.junctions, byID: map[string]*Exit{}}
	for _, j := range b.junctions {
		g.Junctions = append(g.Junctions, j)
	}
	sort.Slice(g.Junctions, func(i, k int) bool { return g.Junctions[i].Stmt.Pos() < g.Junctions[k].Stmt.Pos() })
	for i, j := range g.Junctions {
		j.ID = fmt.Sprintf("J%d", i+1)
		for _, e := range j.Exits {
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
	if b.busy[blk] {
		// Only loops or goto can make a cycle, and those are rejected
		// before building; reaching here is a bug.
		b.fail(fmt.Errorf("internal error: unexpected cycle in %s", b.wf.Name))
		return Target{End: endDead}
	}
	b.busy[blk] = true
	t := b.computeRoad(blk)
	delete(b.busy, blk)
	b.memo[blk] = t
	return t
}

func (b *builder) computeRoad(blk *cfg.Block) Target {
	for _, n := range blk.Nodes {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			return Target{End: b.endKind(ret)}
		}
	}
	switch len(blk.Succs) {
	case 0:
		return Target{End: endDead} // a panic, os.Exit, ...
	case 1:
		return b.road(blk.Succs[0])
	}
	then, other := blk.Succs[0], blk.Succs[1]
	ifs, ok := then.Stmt.(*ast.IfStmt)
	if !ok || then.Kind != cfg.KindIfThen {
		b.fail(fmt.Errorf("internal error: unexpected branch (%s) in %s", then.Kind, b.wf.Name))
		return Target{End: endDead}
	}
	return b.decide(ifs, then, other)
}

// decide turns one if statement into a junction, or walks straight
// through it when it is transparent or ignored (CLAUDE.md D2).
func (b *builder) decide(ifs *ast.IfStmt, then, other *cfg.Block) Target {
	pragma := b.pragmas[b.line(ifs)]
	if pragma == "" {
		pragma = b.pragmas[b.line(ifs)-1]
	}

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
	j := b.newJunction(ifs, PlainIf, "if "+types.ExprString(ifs.Cond))
	j.Exits = []*Exit{
		{Label: "true", Junction: j, Road: ifs.Body},
		{Label: "false", Junction: j, Road: ifs.Else},
	}
	j.Exits[0].To = b.road(then)
	j.Exits[1].To = b.road(other)
	return Target{Junction: j}
}

func (b *builder) transparent(takeThen bool, then, other *cfg.Block) Target {
	if takeThen {
		return b.road(then)
	}
	return b.road(other)
}

func (b *builder) newJunction(ifs *ast.IfStmt, kind JunctionKind, label string) *Junction {
	j := &Junction{Kind: kind, Label: label, Stmt: ifs, Pos: b.fset.Position(ifs.Pos())}
	b.junctions[ifs] = j
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
	ast.Inspect(b.wf.Func.Body, func(n ast.Node) bool {
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
	return rhs, idx, found
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
