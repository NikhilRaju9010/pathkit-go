package model

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
)

// BranchIDPrefix starts every branch ID ("b:" and 12 hex digits).
const BranchIDPrefix = "b:"

// A Branch is one outcome of one decision, told apart by words: the file
// (relative to the module), the function the decision is written in, the
// decision's text, the outcome's label and, for identical decisions in one
// function, their order. It has no line number and no J-number, so it stays
// the same when unrelated code moves. A helper's decision has one Branch
// however many places call the helper (copied in or summarized): Exits holds
// its exit in every copy.
type Branch struct {
	ID       string
	File     string // relative to the module, slash-separated
	Line     int    // where the decision is (its first copy)
	Func     string // the function the decision is written in
	Decision string // the decision's text, whitespace collapsed
	Outcome  string // the exit's label
	Kind     JunctionKind
	// Helper is true for a decision written in a helper function.
	Helper bool
	// Cleanup is true for a decision that runs under a disconnected context: the
	// cleanup after a cancel (see InCleanup).
	Cleanup bool
	// Exits are this branch in the graph: one for a decision in the workflow
	// function, one per call site for a helper copied in.
	Exits []*Exit
}

// BranchID is the ID of the outcome an exit stands for ("" for a retry edge,
// which is not an outcome).
func (g *Graph) BranchID(e *Exit) string {
	g.Branches()
	return g.branchByExit[e]
}

type branchDecl struct{ file, fn, words string }

type srcPos struct{ line, col int }

// Branches lists every outcome of every decision of the workflow and of its
// summarized helpers, once each, in source order of the first copy. A loop's
// retry edge is not an outcome. The list is the same on every call.
func (g *Graph) Branches() []*Branch {
	if g.branches != nil {
		return g.branches
	}
	type item struct {
		j *Junction
		d branchDecl
	}
	var items []item
	groups := map[branchDecl]map[srcPos]int{}
	for _, j := range g.AllJunctions() {
		d := branchDecl{g.relFile(j.Pos.Filename), g.funcName(j), strings.Join(strings.Fields(j.Label), " ")}
		items = append(items, item{j, d})
		if groups[d] == nil {
			groups[d] = map[srcPos]int{}
		}
		groups[d][srcPos{j.Pos.Line, j.Pos.Column}] = 0
	}
	// Identical decisions of one function are told apart by their order in the source.
	for _, m := range groups {
		ps := make([]srcPos, 0, len(m))
		for p := range m {
			ps = append(ps, p)
		}
		sort.Slice(ps, func(a, b int) bool {
			if ps[a].line != ps[b].line {
				return ps[a].line < ps[b].line
			}
			return ps[a].col < ps[b].col
		})
		for i, p := range ps {
			m[p] = i + 1
		}
	}
	byID := map[string]*Branch{}
	g.branchByExit = map[*Exit]string{}
	g.occOf = map[*Junction]int{}
	g.declOf = map[*Junction]branchDecl{}
	g.branches = []*Branch{}
	for _, it := range items {
		occ := groups[it.d][srcPos{it.j.Pos.Line, it.j.Pos.Column}]
		g.occOf[it.j] = occ
		g.declOf[it.j] = it.d
		for _, e := range it.j.Exits {
			outcome := strings.Join(strings.Fields(e.Label), " ")
			sum := sha256.Sum256([]byte("b1\n" + it.d.file + "\n" + it.d.fn + "\n" + it.d.words + "\n" + outcome + "\n" + strconv.Itoa(occ)))
			id := BranchIDPrefix + hex.EncodeToString(sum[:])[:12]
			b := byID[id]
			if b == nil {
				b = &Branch{ID: id, File: it.d.file, Line: it.j.Pos.Line, Func: it.d.fn, Decision: it.d.words, Outcome: outcome, Kind: it.j.Kind, Helper: it.j.Site != nil, Cleanup: g.InCleanup(it.j)}
				byID[id] = b
				g.branches = append(g.branches, b)
			}
			b.Exits = append(b.Exits, e)
			g.branchByExit[e] = id
		}
	}
	return g.branches
}

func (g *Graph) relFile(name string) string {
	if g.modDir != "" {
		if rel, err := filepath.Rel(g.modDir, name); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(name)
}

// moduleDir is the folder of the module the workflow's package is in.
func moduleDir(wf discover.Workflow) string {
	if wf.Pkg != nil && wf.Pkg.Module != nil {
		return wf.Pkg.Module.Dir
	}
	return ""
}

// funcName is the function a decision is written in: the workflow function,
// or the helper (with its receiver type for a method).
func (g *Graph) funcName(j *Junction) string {
	if j.Site == nil {
		return funcDeclName(g.fn)
	}
	if d := j.Site.Decl; d != nil {
		return funcDeclName(d)
	}
	return j.Site.Helper
}

func funcDeclName(d *ast.FuncDecl) string {
	if d == nil {
		return ""
	}
	name := d.Name.Name
	if d.Recv != nil && len(d.Recv.List) > 0 {
		t := d.Recv.List[0].Type
		if s, ok := t.(*ast.StarExpr); ok {
			t = s.X
		}
		if ix, ok := t.(*ast.IndexExpr); ok {
			t = ix.X
		}
		if id, ok := t.(*ast.Ident); ok {
			name = id.Name + "." + name
		}
	}
	return name
}

// ModulePackage returns the package with the given import path when it belongs
// to the same Go module as the workflow and its source was read in this run
// (it was among the packages PathKit was asked to read); nil otherwise. The
// fact engine uses it to read the code of plain functions the workflow calls.
func (g *Graph) ModulePackage(path string) *packages.Package {
	if g.wfPkg == nil || g.wfPkg.Module == nil {
		return nil
	}
	if g.modPkgs == nil {
		g.modPkgs = map[string]*packages.Package{}
		var walk func(p *packages.Package)
		walk = func(p *packages.Package) {
			if _, seen := g.modPkgs[p.PkgPath]; seen {
				return
			}
			g.modPkgs[p.PkgPath] = nil
			if p.Module != nil && p.Module.Path == g.wfPkg.Module.Path && len(p.Syntax) > 0 && p.TypesInfo != nil {
				g.modPkgs[p.PkgPath] = p
			}
			for _, imp := range p.Imports {
				walk(imp)
			}
		}
		walk(g.wfPkg)
	}
	return g.modPkgs[path]
}
