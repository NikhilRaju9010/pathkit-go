package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/cfg"
)

// HelperMode says how the code of helper functions (functions of the
// workflow's package that call the Temporal SDK) is put on the map.
type HelperMode int

const (
	// HelpersInline copies a helper's code into the map at every call, as
	// PathKit always did (docs/DESIGN.md D15): exact, but a workflow that calls
	// a helper with a few decisions at many places has a product of them as
	// paths.
	HelpersInline HelperMode = iota
	// HelpersSummarize makes a helper call one step of the workflow (the call,
	// and the error check on its result when there is one). The helper's own
	// decisions are mapped once, as a HelperUnit shared by every call, and
	// covered by any call.
	HelpersSummarize
	// HelpersAuto inlines while the workflow stays within the path cap, and
	// summarizes when inlining would pass it.
	HelpersAuto
)

// Options are the choices Build can be given.
type Options struct {
	Helpers HelperMode
	// PathCap is the path cap HelpersAuto compares the exact path count with;
	// 0 means DefaultMaxPaths.
	PathCap int
	// DirectReturns makes "return fut.Get(...)", "return workflow.Sleep(...)"
	// and "return workflow.Await(...)" decisions (classification.
	// direct_returns: "step"). Off, they stay a path with an unknown end and are
	// listed as unread.
	DirectReturns bool
	// Wrappers are functions to treat as a call that starts and waits for an
	// activity, a child workflow or a Nexus operation (classification.
	// activity_wrappers).
	Wrappers []Wrapper
}

// Wrapper is a function PathKit is told runs a Temporal call for its caller.
type Wrapper struct {
	// Function is the name as wrapperName writes it: "pkg/path.Func" or
	// "(*pkg/path.Type).Method".
	Function string
	// Kind is "activity", "child_workflow" or "nexus".
	Kind string
	// Label names the step; empty means the function's own name.
	Label string
}

// A HelperUnit is the map of one helper function that a workflow calls in
// summarize mode: its decisions, once, however many places call it. Its
// branches are covered when any call takes them. It is not part of the
// workflow's paths: at a call the workflow takes one step, whatever the
// helper does inside.
type HelperUnit struct {
	// ID names the unit in the marked-up code and in traces ("H1", "H2", ...).
	ID string
	// Site is the unit's code: Body, Decl, Key and the helper's name.
	Site *Site
	// Calls are the places that call the helper, in the workflow function or
	// inside another unit.
	Calls []UnitCall
	// Junctions are the helper's decisions, numbered "H1.J1", "H1.J2", ...
	Junctions []*Junction
}

// UnitCall is one call of a helper that is summarized.
type UnitCall struct {
	Call *ast.CallExpr
	// Ctx is the argument that carries the workflow context.
	Ctx ast.Expr
	// From is the unit whose code holds the call; nil for the workflow function.
	From *HelperUnit
}

// Name is the helper as written in the first call: "charge", "o.charge".
func (u *HelperUnit) Name() string { return u.Site.Helper }

// Branches are the exits of the unit's decisions (a loop's retry edge is not
// a branch), as exit IDs.
func (u *HelperUnit) Branches() []EdgeID {
	var out []EdgeID
	for _, j := range u.Junctions {
		for _, e := range j.Exits {
			out = append(out, e.ID)
		}
	}
	return out
}

// Position is where the helper is declared.
func (u *HelperUnit) Position() token.Position { return u.Site.Pos }

// Units lists the summarized helpers in the order of their first call.
func (g *Graph) Units() []*HelperUnit { return g.units }

// Summarized reports whether the graph was built in summarize mode, and
// SummarizedBecause why ("" when it was not).
func (g *Graph) Summarized() bool { return g.summarized }

// SummarizedBecause says in a line why the helpers were summarized.
func (g *Graph) SummarizedBecause() string { return g.summarizedWhy }

// UnitCalls lists every call of a summarized helper, for marking up the code.
func (g *Graph) UnitCalls() []UnitCall {
	var out []UnitCall
	for _, u := range g.units {
		out = append(out, u.Calls...)
	}
	return out
}

// AllJunctions are the workflow's decisions followed by those of its
// summarized helpers.
func (g *Graph) AllJunctions() []*Junction {
	out := append([]*Junction(nil), g.Junctions...)
	for _, u := range g.units {
		out = append(out, u.Junctions...)
	}
	return out
}

// LookupUnitEdge finds an exit of a summarized helper by its ID ("H1.J2.true").
func (g *Graph) LookupUnitEdge(s string) (*Exit, bool) {
	e, ok := g.unitByID[s]
	return e, ok
}

// BuildWith makes the junction map of one workflow with the given options.
// Build is BuildWith inlining helpers.
func BuildWith(wf discoverWorkflow, opt Options) (*Graph, error) {
	build := func(wf discoverWorkflow, summarize bool) (*Graph, error) {
		return buildWith(wf, summarize, opt)
	}
	switch opt.Helpers {
	case HelpersSummarize:
		g, err := build(wf, true)
		if err != nil {
			return nil, err
		}
		var once *Graph
		g.inline = func() *Graph {
			if once == nil {
				once, _ = build(wf, false)
			}
			return once
		}
		return g, nil
	case HelpersAuto:
		g, err := build(wf, false)
		if err != nil {
			return nil, err
		}
		limit := opt.PathCap
		if limit <= 0 {
			limit = DefaultMaxPaths
		}
		if !g.UsesHelpers() || g.PathCount().Cmp(bigInt(limit)) <= 0 {
			return g, nil
		}
		s, err := build(wf, true)
		if err != nil {
			return g, nil // keep the exact map rather than fail
		}
		s.summarized = true
		s.inline = func() *Graph { return g }
		s.summarizedWhy = fmt.Sprintf("copying the helpers into the map gives %s paths, more than the path cap of %d", groupDigits(g.PathCount().String()), limit)
		return s, nil
	}
	return build(wf, false)
}

// unitCallee decides whether a call is a call of a helper that gets a unit:
// the helper is a function of the workflow's package that does workflow work,
// its code holds nothing PathKit cannot map, it is not a workflow of its own,
// and the call passes the workflow context in a named parameter.
func (b *builder) unitCallee(call *ast.CallExpr) (*helperFn, ast.Expr) {
	if _, ok := b.wrapperFor(call); ok {
		return nil, nil // told to be a Temporal call, not a helper to read
	}
	h := b.calleeOf(call)
	if h == nil || call.Ellipsis != token.NoPos || h.body == b.wf.Func.Body {
		return nil, nil
	}
	if b.isWorkflowFunc(h) {
		return nil, nil
	}
	idx, _ := b.ctxParam(h.typ)
	if idx < 0 || idx >= len(call.Args) {
		return nil, nil
	}
	if !b.helperClean(h) || !b.worthInlining(h, 0) {
		return nil, nil
	}
	return h, call.Args[idx]
}

// buildUnits finds the helpers the workflow calls (and the ones they call, up
// to maxHelperDepth deep) and maps each once.
func (b *builder) buildUnits() {
	var stack []*HelperUnit
	var scan func(body *ast.BlockStmt, from *HelperUnit)
	scan = func(body *ast.BlockStmt, from *HelperUnit) {
		ast.Inspect(body, func(n ast.Node) bool {
			if _, isLit := n.(*ast.FuncLit); isLit {
				return false // a closure's calls run later, from a place PathKit does not follow
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			h, ctx := b.unitCallee(call)
			if h == nil {
				return true
			}
			for _, u := range stack {
				if u.Site.Body == h.body {
					return true // a helper calling itself, or the helper it was reached from
				}
			}
			u := b.unitOf[h.body]
			if u == nil {
				if len(stack) >= maxHelperDepth {
					return true
				}
				u = b.newUnit(h, call, ctx)
				stack = append(stack, u)
				b.buildUnitBody(u)
				scan(h.body, u)
				stack = stack[:len(stack)-1]
			}
			u.Calls = append(u.Calls, UnitCall{Call: call, Ctx: ctx, From: from})
			return true
		})
	}
	scan(b.wf.Func.Body, nil)
}

func (b *builder) newUnit(h *helperFn, call *ast.CallExpr, ctx ast.Expr) *HelperUnit {
	_, ctxName := b.ctxParam(h.typ)
	s := &Site{Helper: h.name, Call: call, Ctx: ctx, CtxName: ctxName, Type: h.typ, Body: h.body, File: h.file, Decl: h.decl,
		Pos: b.fset.Position(h.body.Pos()), order: []token.Pos{call.Pos()}, h: h}
	p := b.fset.Position(h.body.Pos())
	s.Key = fmt.Sprintf("%s:%d:%d", filepath.Base(p.Filename), p.Line, p.Column)
	if h.decl != nil {
		s.Pos = b.fset.Position(h.decl.Pos())
	}
	u := &HelperUnit{Site: s}
	s.Unit = u
	if b.unitOf == nil {
		b.unitOf = map[*ast.BlockStmt]*HelperUnit{}
	}
	b.unitOf[h.body] = u
	b.units = append(b.units, u)
	return u
}

// buildUnitBody walks the helper's code once, making its junctions. A return
// in it just ends the walk: where it goes next does not matter to the unit.
func (b *builder) buildUnitBody(u *HelperUnit) {
	c := cfg.New(u.Site.Body, b.mayReturn)
	done := func(retKind) Target { return Target{End: EndCompleted} }
	for _, cb := range c.Blocks {
		b.contOf[cb] = done
		b.siteOf[cb] = u.Site
	}
	prev := b.cur
	b.cur = u.Site
	b.indexSwitchesIn(u.Site.Body)
	b.indexLoops(c)
	b.road(c.Blocks[0])
	b.cur = prev
}

// finishUnits numbers the units and their decisions, and takes the unit
// junctions out of the workflow's own list.
func (b *builder) finishUnits(g *Graph) {
	sort.SliceStable(b.units, func(i, k int) bool { return lessOrder(b.units[i].Site.order, b.units[k].Site.order) })
	g.unitByID = map[string]*Exit{}
	for i, u := range b.units {
		u.ID = fmt.Sprintf("H%d", i+1)
		u.Site.ID = u.ID
		g.units = append(g.units, u)
	}
	for _, j := range b.junctions {
		if j.Site != nil && j.Site.Unit != nil {
			u := j.Site.Unit
			u.Junctions = append(u.Junctions, j)
		}
	}
	for _, u := range g.units {
		sort.SliceStable(u.Junctions, func(i, k int) bool { return lessOrder(u.Junctions[i].order, u.Junctions[k].order) })
		for i, j := range u.Junctions {
			j.ID = fmt.Sprintf("%s.J%d", u.ID, i+1)
			exits := j.Exits
			if j.Retry != nil {
				exits = append(exits[:len(exits):len(exits)], j.Retry)
			}
			for _, e := range exits {
				e.ID = EdgeID{j.ID + "." + e.Label}
				g.unitByID[e.ID.s] = e
			}
		}
	}
}

// InlineHash is the CodeHash a trace has when it was recorded against the map
// that copies the helpers in (what PathKit recorded before it summarized
// helpers); "" when this map is not a summarized one.
func (g *Graph) InlineHash() string {
	if g.inline == nil {
		return ""
	}
	if in := g.inline(); in != nil {
		return in.CodeHash()
	}
	return ""
}

// FromInline turns the steps of a trace recorded against the map that copies
// helpers in into the steps of this map, so a trace recorded before helpers
// were summarized still counts. The workflow's own decisions are found by the
// statement they come from, and the helpers' decisions become the hits of
// their units. ok is false when some step has no counterpart here; the trace
// is then not read (it is reported as recorded for another version).
func (g *Graph) FromInline(raw []string) (out []string, ok bool) {
	if g.inline == nil {
		return nil, false
	}
	in := g.inline()
	if in == nil {
		return nil, false
	}
	known := map[string]*Site{}
	for _, s := range in.sites {
		known[s.ID] = s
	}
	unitOfBody := map[*ast.BlockStmt]*HelperUnit{}
	for _, u := range g.units {
		unitOfBody[u.Site.Body] = u
	}
	label := func(e *Exit) string { return e.Label }
	find := func(j *Junction, lab string) *Exit {
		for _, e := range j.Exits {
			if label(e) == lab {
				return e
			}
		}
		if j.Retry != nil && label(j.Retry) == lab {
			return j.Retry
		}
		return nil
	}
	var stack []*HelperUnit // the unit each open site maps to (nil: the workflow itself)
	for _, s := range raw {
		switch {
		case len(s) > 1 && s[0] == '>':
			site := known[s[1:]]
			if site == nil {
				return nil, false
			}
			u := unitOfBody[site.Body]
			if u == nil {
				return nil, false
			}
			stack = append(stack, u)
			out = append(out, ">"+u.ID)
		case len(s) > 1 && s[0] == '<':
			if len(stack) == 0 {
				return nil, false
			}
			out = append(out, "<"+stack[len(stack)-1].ID)
			stack = stack[:len(stack)-1]
		default:
			e, found := in.LookupEdge(s)
			if !found {
				return nil, false
			}
			j := e.Junction
			if j.Site == nil {
				if len(stack) != 0 {
					return nil, false
				}
				j2 := g.byStmt[j.Stmt]
				if j2 == nil {
					return nil, false
				}
				e2 := find(j2, e.Label)
				if e2 == nil {
					return nil, false
				}
				out = append(out, e2.ID.String())
				continue
			}
			if len(stack) == 0 {
				return nil, false
			}
			u := stack[len(stack)-1]
			var e2 *Exit
			for _, uj := range u.Junctions {
				if uj.Stmt == j.Stmt {
					e2 = find(uj, e.Label)
				}
			}
			if e2 == nil {
				return nil, false
			}
			out = append(out, e2.ID.String())
		}
	}
	if len(stack) != 0 {
		return nil, false
	}
	return out, true
}

// groupDigits writes 8191000 as 8,191,000.
func groupDigits(s string) string {
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
