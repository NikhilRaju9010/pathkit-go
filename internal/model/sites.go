package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
)

// A Site is one copy of a helper's code inside a workflow's graph
// (docs/DESIGN.md D15). A helper is a function of the workflow's package
// that the workflow calls and that makes decisions or calls the Temporal
// SDK. Each call of it is a site: its decisions are junctions of their own,
// numbered and recorded separately from the decisions of any other call of
// the same helper. The workflow function itself is not a site (its
// junctions have a nil Site).
type Site struct {
	// ID names the site in the marked-up copies of the code, "S1", "S2", ...
	ID string
	// Parent is the site whose code holds the call; nil for the workflow.
	Parent *Site
	// Helper is the callee as written at the call: "charge", "o.charge".
	Helper string
	// Call is the call, and Ctx its argument that carries the workflow
	// context (the recorder hands the site's identity over through it).
	Call *ast.CallExpr
	Ctx  ast.Expr
	// CtxName is the helper's own name for that context: the parameter the
	// marked-up copy of the helper reads the recorder's hand-over from.
	CtxName *ast.Ident
	// Type, Body, File and Decl/Lit describe the helper's code.
	Type *ast.FuncType
	Body *ast.BlockStmt
	File *ast.File
	Decl *ast.FuncDecl // nil for a function literal
	// Key names the helper in the recorder, so a copy of the code entered
	// from anywhere else records nothing.
	Key string
	// Pos is where the call is.
	Pos token.Position

	order []token.Pos // the positions of the calls that lead here
	h     *helperFn
}

// Depth is how many helpers deep the site is: 1 for a call in the workflow.
func (s *Site) Depth() int {
	n := 0
	for ; s != nil; s = s.Parent {
		n++
	}
	return n
}

// Words writes the way into the site in plain words, for stable IDs:
// "in charge called as charge(ctx, o)", then the same for the site it is in.
func (s *Site) Words() string {
	var parts []string
	for ; s != nil; s = s.Parent {
		parts = append(parts, "in "+s.Helper+" called as "+strings.Join(strings.Fields(types.ExprString(s.Call)), " "))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}

// Sites lists the copies of helper code in the graph, in the order their
// calls come in the code.
func (g *Graph) Sites() []*Site { return g.sites }

// UsesHelpers reports whether the graph has any copy of helper code.
func (g *Graph) UsesHelpers() bool { return len(g.sites) > 0 }

// LocalKey names one exit of the decision stmt inside a helper, the way the
// marked-up copy of the helper records it. The recorder turns it into the
// exit's ID with the site's table (SiteExits).
func (g *Graph) LocalKey(stmt ast.Node, label string) string {
	p := g.fset.Position(stmt.Pos())
	return fmt.Sprintf("%d:%d.%s", p.Line, p.Column, label)
}

// SiteExit is one row of a site's table: in the copy of helper code at
// Site, the exit LocalKey is the graph's exit ID.
type SiteExit struct {
	Site     *Site
	LocalKey string
	ID       EdgeID
}

// SiteExits lists the table that turns what a helper records into exit IDs.
func (g *Graph) SiteExits() []SiteExit {
	var out []SiteExit
	for _, j := range g.Junctions {
		if j.Site == nil {
			continue
		}
		exits := j.Exits
		if j.Retry != nil {
			exits = append(exits[:len(exits):len(exits)], j.Retry)
		}
		for _, e := range exits {
			out = append(out, SiteExit{Site: j.Site, LocalKey: g.LocalKey(j.Stmt, e.Label), ID: e.ID})
		}
	}
	return out
}

// CodeHash fingerprints the code a trace of this graph depends on: the
// workflow function (FunctionHash) and, when helper code is part of the
// graph, the declared helper functions that are copied into it. A workflow
// with no helper code keeps exactly the FunctionHash it always had.
func (g *Graph) CodeHash() string {
	h := FunctionHash(g.fset, g.fn)
	var extra []string
	seen := map[*ast.FuncDecl]bool{}
	for _, s := range g.sites {
		if s.Decl != nil && !seen[s.Decl] {
			seen[s.Decl] = true
			extra = append(extra, filepath.Base(g.fset.Position(s.Decl.Pos()).Filename)+":"+FunctionHash(g.fset, s.Decl))
		}
	}
	if len(extra) == 0 {
		return h
	}
	sort.Strings(extra)
	return hashText(h + "\n" + strings.Join(extra, "\n"))
}

// CallKey names a call inside the code that holds it, the way the marked-up
// copy of that code hands the call's site over (see RecorderTable).
func (g *Graph) CallKey(call *ast.CallExpr) string {
	p := g.fset.Position(call.Pos())
	return fmt.Sprintf("c%d:%d", p.Line, p.Column)
}

// TableKey joins a site ID ("" for the workflow function) and a local key
// the way the recorder looks them up.
func TableKey(site, local string) string { return site + "|" + local }

// RecorderTable is everything the recorder needs to turn what marked-up
// helper code records into this graph's exit IDs, as one lookup table:
//   - TableKey(parent, CallKey(call)) is the ID of the site that call leads to;
//   - TableKey(site, "") is the key of the helper whose code the site is;
//   - TableKey(site, LocalKey(stmt, label)) is the ID of an exit in that code.
func (g *Graph) RecorderTable() map[string]string {
	t := map[string]string{}
	for _, s := range g.sites {
		parent := ""
		if s.Parent != nil {
			parent = s.Parent.ID
		}
		t[TableKey(parent, g.CallKey(s.Call))] = s.ID
		t[TableKey(s.ID, "")] = s.Key
	}
	for _, e := range g.SiteExits() {
		t[TableKey(e.Site.ID, e.LocalKey)] = e.ID.String()
	}
	return t
}

// Helpers lists the helper functions whose code is copied into the graph,
// each once, in the order of their first call. Their code must be marked up.
func (g *Graph) Helpers() []*Site {
	var out []*Site
	seen := map[*ast.BlockStmt]bool{}
	for _, s := range g.sites {
		if !seen[s.Body] {
			seen[s.Body] = true
			out = append(out, s)
		}
	}
	return out
}

// Where says, for a decision inside a helper, which helper it is in:
// " [in charge]", and for one a helper deeper " [in inner, in charge]".
// It is "" for a decision in the workflow function itself.
func (j *Junction) Where() string {
	if j.Site == nil {
		return ""
	}
	var names []string
	for s := j.Site; s != nil; s = s.Parent {
		names = append(names, "in "+s.Helper)
	}
	return " [" + strings.Join(names, ", ") + "]"
}

// HelperNote says why helper functions are not followed in this graph, when
// following them would have given more paths than the path cap: "" when they
// are followed (or there are none).
func (g *Graph) HelperNote() string { return g.helperNote }
