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
	pragmas   map[pragmaAt]string // line -> "ignore" / "branch"
	memo      map[*cfg.Block]Target
	busy      map[*cfg.Block]bool
	junctions map[jkey]*Junction
	switchOf  map[*ast.CaseClause]ast.Stmt // case clause -> its switch
	loopHeads map[*cfg.Block]*loopInfo     // a loop's head block -> the loop
	selectAt  map[ast.Stmt]*selectorInfo   // a checked Select statement -> its selector
	// contOf marks the blocks of a selector callback: a return in the
	// callback, or its end, goes on to the code after the Select call.
	contOf map[*cfg.Block]func(retKind) Target
	memoAt map[blockPos]Target // roads that start in the middle of a block
	err    error
	decls  map[*types.Func]*helperFn // see helperDecls

	// cur is the copy of helper code being walked (nil for the workflow
	// function); siteOf says which copy each block of a go/cfg graph is in.
	cur    *Site
	siteOf map[*cfg.Block]*Site
	sites  map[siteKey]*Site
	// worth remembers worthInlining per helper body; worthBusy guards a cycle.
	worth       map[*ast.BlockStmt]bool
	worthBusy   map[*ast.BlockStmt]bool
	worthCycles int
	// siteList is every site, in the order they were made.
	siteList []*Site

	// summarize: helper calls become one step each and the helpers are mapped
	// once, as units (see HelperUnit), instead of being copied in.
	unitMode bool
	// wrappers are the functions classification.activity_wrappers names.
	wrappers map[string]Wrapper
	// directReturns: a "return <call>" is a decision (Options.DirectReturns).
	directReturns bool
	// sawDirectReturn: a "return <call>" became a decision.
	sawDirectReturn bool
	units           []*HelperUnit
	unitOf          map[*ast.BlockStmt]*HelperUnit
}

// jkey names a junction: a decision in the code, in one copy of it.
type jkey struct {
	site *Site
	node ast.Node
}

// siteKey names a site: a call, inside one copy of code.
type siteKey struct {
	parent *Site
	call   *ast.CallExpr
}

// key is the junction key of stmt in the code being walked.
func (b *builder) key(stmt ast.Node) jkey { return jkey{b.cur, stmt} }

// blockPos is a place inside a block: before its node i.
type blockPos struct {
	blk *cfg.Block
	i   int
	det determined
}

// Build makes the junction map of one workflow. Helper functions of the
// workflow's package are followed (docs/DESIGN.md D15), however many paths
// that gives: the map is counted, cut into stages and covered without listing
// every path (D16).
func Build(wf discover.Workflow) (*Graph, error) { return buildWith(wf, false, Options{}) }

// discoverWorkflow lets units.go name the type without a second import.
type discoverWorkflow = discover.Workflow

func buildWith(wf discover.Workflow, summarize bool, opt Options) (*Graph, error) {
	b := newBuilder(wf)
	b.unitMode = summarize
	b.wrappers = map[string]Wrapper{}
	for _, w := range opt.Wrappers {
		b.wrappers[w.Function] = w
	}
	b.directReturns = opt.DirectReturns
	if u := b.findUnsupported(); u != nil {
		return nil, u
	}

	c := cfg.New(wf.Func.Body, b.mayReturn)
	b.indexLoops(c)
	start := b.road(c.Blocks[0])
	if b.err != nil {
		return nil, b.err
	}
	if b.unitMode {
		b.buildUnits()
		if b.err != nil {
			return nil, b.err
		}
	}
	g := b.finish(start)
	g.summarized = b.unitMode
	g.directReturnSteps = b.sawDirectReturn
	if err := b.attachMarks(g); err != nil {
		return nil, err
	}
	b.attachFacts(g)
	g.unread = b.collectUnread()
	return g, nil
}

func newBuilder(wf discover.Workflow) *builder {
	b := &builder{
		wf:        wf,
		info:      wf.Pkg.TypesInfo,
		fset:      wf.Pkg.Fset,
		memo:      map[*cfg.Block]Target{},
		busy:      map[*cfg.Block]bool{},
		junctions: map[jkey]*Junction{},
		siteOf:    map[*cfg.Block]*Site{},
		sites:     map[siteKey]*Site{},
		worth:     map[*ast.BlockStmt]bool{},
		worthBusy: map[*ast.BlockStmt]bool{},
		switchOf:  map[*ast.CaseClause]ast.Stmt{},
		loopHeads: map[*cfg.Block]*loopInfo{},
		selectAt:  map[ast.Stmt]*selectorInfo{},
		contOf:    map[*cfg.Block]func(retKind) Target{},
		memoAt:    map[blockPos]Target{},
	}
	b.pragmas = b.readPragmas()
	b.indexSwitches()
	return b
}

// finish numbers the junctions in source order and assigns every ID.
func (b *builder) finish(start Target) *Graph {
	g := &Graph{Workflow: b.wf.Name, Start: start, byStmt: map[ast.Node]*Junction{}, byID: map[string]*Exit{}, info: b.info, fn: b.wf.Func, fset: b.fset, pkg: b.wf.Pkg.Types,
		trusted: func(e ast.Expr) bool { return b.trustedConstant(e, map[types.Object]bool{}) }}
	for _, j := range b.junctions {
		if j.Site != nil && j.Site.Unit != nil {
			continue // a summarized helper's decision: numbered with its unit
		}
		g.Junctions = append(g.Junctions, j)
		if j.Site == nil {
			g.byStmt[j.Stmt] = j
		}
	}
	sort.SliceStable(g.Junctions, func(i, k int) bool { return lessOrder(g.Junctions[i].order, g.Junctions[k].order) })
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
	// Every site made while building is kept: a call is followed once, so a site is never abandoned.
	g.sites = append([]*Site(nil), b.siteList...)
	sort.SliceStable(g.sites, func(i, k int) bool { return lessOrder(g.sites[i].order, g.sites[k].order) })
	for i, s := range g.sites {
		s.ID = fmt.Sprintf("S%d", i+1)
	}
	g.assignStable()
	b.finishUnits(g)
	return g
}

// lessOrder compares two numbering keys (see Junction.order).
func lessOrder(a, b []token.Pos) bool {
	for i := range a {
		if i >= len(b) {
			return false
		}
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// orderAt is the numbering key of a decision that starts at pos in the
// code being walked.
func (b *builder) orderAt(pos token.Pos) []token.Pos {
	var base []token.Pos
	if b.cur != nil {
		base = b.cur.order
	}
	return append(base[:len(base):len(base)], pos)
}

// road returns where execution goes from the start of blk.
func (b *builder) road(blk *cfg.Block) Target {
	if t, ok := b.memo[blk]; ok {
		return t
	}
	defer b.enterBlock(blk)()
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
	return b.roadFromDet(blk, i, determined{})
}

// roadFromDet is roadFrom for a road that comes back from a helper that
// already decided the caller's error check (see determined).
func (b *builder) roadFromDet(blk *cfg.Block, i int, det determined) Target {
	if i == 0 && det.obj == nil {
		return b.road(blk)
	}
	at := blockPos{blk, i, det}
	if t, ok := b.memoAt[at]; ok {
		return t
	}
	t := b.computeRoadFrom(blk, i, det)
	b.memoAt[at] = t
	return t
}

// enterBlock makes the code of blk the code being walked, and returns the
// call that puts the previous one back.
func (b *builder) enterBlock(blk *cfg.Block) func() {
	prev := b.cur
	b.cur = b.siteOf[blk]
	return func() { b.cur = prev }
}

func (b *builder) computeRoad(blk *cfg.Block) Target { return b.computeRoadFrom(blk, 0, determined{}) }

// computeRoadFrom is where execution goes from before node from of blk.
// A road that passes a saga compensation defer carries the note (docs/DESIGN.md
// D3: noted, never a branch).
func (b *builder) computeRoadFrom(blk *cfg.Block, from int, det determined) Target {
	defer b.enterBlock(blk)()
	t, passed, upto := b.roadThrough(blk, from, det)
	if passed {
		t.Compensation = true
	}
	// The statements this road runs before it reaches t (see Target.Nodes).
	// A fresh slice: t may be a memoized Target other roads share.
	t.Nodes = append(append([]ast.Node(nil), blk.Nodes[from:upto]...), t.Nodes...)
	return t
}

// roadThrough does the work of computeRoadFrom; passed reports whether
// the nodes it went through register a compensation defer, and upto is
// how far into blk.Nodes the road runs before its target (the nodes
// from..upto-1 are executed; a Select call is the junction, not a node
// that is run before it).
func (b *builder) roadThrough(blk *cfg.Block, from int, det determined) (t Target, passed bool, upto int) {
	cont := b.contOf[blk] // set inside a selector callback or a helper
	for i, n := range blk.Nodes[from:] {
		// A defer in a selector callback runs when the callback ends, not
		// when the workflow does: only the workflow's own defers count.
		if d, ok := n.(*ast.DeferStmt); ok && cont == nil && b.isCompensation(d) {
			passed = true
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			if ic := b.inlineAt(n); ic != nil {
				return b.intoHelper(ic, blk, from+i), passed, from + i + 1
			}
			if t, ok := b.returnCallJunction(ret, cont, blk); ok {
				return t, passed, from + i + 1
			}
			if cont != nil {
				return cont(b.returnedKind(blk, ret)), passed, from + i + 1 // returns from the callback or helper, not the workflow
			}
			return Target{End: b.endKind(ret)}, passed, from + i + 1
		}
		if s, ok := n.(ast.Stmt); ok && b.selectAt[s] != nil {
			return b.selectorJunction(b.selectAt[s], blk, from+i), passed, from + i
		}
		if ic := b.inlineAt(n); ic != nil {
			return b.intoHelper(ic, blk, from+i), passed, from + i + 1
		}
	}
	switch len(blk.Succs) {
	case 0:
		if cont != nil && !b.endsInNoReturn(blk) {
			return cont(b.returnedKind(blk, nil)), passed, len(blk.Nodes) // the callback's or helper's end
		}
		return Target{End: endDead}, passed, len(blk.Nodes) // a panic, os.Exit, ...
	case 1:
		return b.road(blk.Succs[0]), passed, len(blk.Nodes)
	}
	then, other := blk.Succs[0], blk.Succs[1]
	if ifs, ok := then.Stmt.(*ast.IfStmt); ok && then.Kind == cfg.KindIfThen {
		return b.decide(ifs, then, other, det), passed, len(blk.Nodes)
	}
	if clause, ok := then.Stmt.(*ast.CaseClause); ok && then.Kind == cfg.KindSwitchCaseBody {
		if sw := b.switchOf[clause]; sw != nil {
			return b.switchJunction(sw, blk), passed, len(blk.Nodes)
		}
	}
	b.fail(fmt.Errorf("internal error: unexpected branch (%s) in %s", then.Kind, b.wf.Name))
	return Target{End: endDead}, passed, len(blk.Nodes)
}

// decide turns one if statement into a junction, or walks straight
// through it when it is transparent or ignored (docs/DESIGN.md D2).
func (b *builder) decide(ifs *ast.IfStmt, then, other *cfg.Block, det determined) Target {
	pragma := b.ifPragma(ifs)
	if obj, neq, isErrForm := b.errForm(ifs.Cond); isErrForm {
		label, temporal, viaHelper := b.errSource(obj, ifs)
		noErrorIsThen := !neq
		if pragma == "ignore" || (!temporal && pragma != "branch") {
			return b.transparent(ifs, noErrorIsThen, then, other)
		}
		j := b.junctions[b.key(ifs)]
		if j == nil {
			j = b.newJunction(ifs, ErrCheck, label)
			j.viaHelper = viaHelper
			j.call = b.callPos(obj, ifs)
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
		}
		t := Target{Junction: j}
		if det.obj == obj {
			// A helper just returned, and which error it returned is known.
			t.Only = j.Exits[1]
			if det.fails {
				t.Only = j.Exits[0]
			}
		}
		return t
	}

	if pragma == "ignore" {
		return b.transparent(ifs, false, then, other) // assume a defensive check doesn't fire
	}
	if j := b.junctions[b.key(ifs)]; j != nil {
		return Target{Junction: j} // reached again by another road (see determined)
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
	p := b.fset.Position(ifs.Pos())
	if s := b.pragmas[pragmaAt{p.Filename, p.Line}]; s != "" {
		return s
	}
	return b.pragmas[pragmaAt{p.Filename, p.Line - 1}]
}

// ifIsJunction reports whether decide makes ifs a junction, without
// building anything. It must follow exactly the same rules as decide.
func (b *builder) ifIsJunction(ifs *ast.IfStmt) bool {
	pragma := b.ifPragma(ifs)
	if pragma == "ignore" {
		return false
	}
	if obj, _, isErrForm := b.errForm(ifs.Cond); isErrForm {
		_, temporal, _ := b.errSource(obj, ifs)
		return temporal || pragma == "branch"
	}
	return true
}

func (b *builder) transparent(ifs *ast.IfStmt, takeThen bool, then, other *cfg.Block) Target {
	road := other
	if takeThen {
		road = then
	}
	t := b.road(road)
	// The walk assumes one side of this if; a run may take the other, so
	// nothing the if's code does can be relied on (see SkipMark).
	t.Nodes = append([]ast.Node{SkipMark{Stmt: ifs}}, t.Nodes...)
	return t
}

func (b *builder) newJunction(stmt ast.Node, kind JunctionKind, label string) *Junction {
	j := &Junction{Kind: kind, Label: label, Stmt: stmt, Pos: b.fset.Position(stmt.Pos()), order: b.orderAt(stmt.Pos()), Site: b.cur}
	b.junctions[b.key(stmt)] = j
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
	root := b.rootBody(pos)
	out := []*ast.BlockStmt{root}
	ast.Inspect(root, func(n ast.Node) bool {
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

// rootBody is the body of the function declaration pos is inside: the
// workflow function, or a helper of its package.
func (b *builder) rootBody(pos token.Pos) *ast.BlockStmt {
	if pos >= b.wf.Func.Pos() && pos < b.wf.Func.End() {
		return b.wf.Func.Body
	}
	for _, h := range b.helperDecls() {
		if h.body.Pos() <= pos && pos < h.body.End() {
			return h.body
		}
	}
	return b.wf.Func.Body
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
	return b.knownNonNilFile(b.wf.File, obj, ret)
}

// knownNonNilFile is knownNonNil for a return statement in file.
func (b *builder) knownNonNilFile(file *ast.File, obj types.Object, ret *ast.ReturnStmt) bool {
	path, _ := astutil.PathEnclosingInterval(file, ret.Pos(), ret.End())
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

func (b *builder) readPragmas() map[pragmaAt]string {
	m := map[pragmaAt]string{}
	for _, f := range b.wf.Pkg.Syntax {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				p := b.fset.Position(c.Slash)
				switch {
				case strings.HasPrefix(c.Text, "//pathkit:ignore"):
					m[pragmaAt{p.Filename, p.Line}] = "ignore"
				case strings.HasPrefix(c.Text, "//pathkit:branch"):
					m[pragmaAt{p.Filename, p.Line}] = "branch"
				}
			}
		}
	}
	return m
}

// pragmaAt is a line of a source file.
type pragmaAt struct {
	file string
	line int
}

func (b *builder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}
