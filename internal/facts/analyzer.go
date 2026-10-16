package facts

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Analyzer analyzes the paths of one workflow.
type Analyzer struct {
	g    *model.Graph
	info *types.Info
	fset *token.FileSet
	// off is why the engine does nothing for this function ("" when on).
	off string
	// untracked are variables the engine never keeps facts about, with the
	// reason in plain words.
	untracked map[*types.Var]string
	// assignedIn caches the variables assigned anywhere inside a statement.
	assignedIn map[ast.Node]map[*types.Var]bool
	// calls remembers, per node, whether it holds a call (see mayCall).
	calls map[ast.Node]bool
}

// New prepares the engine for one workflow's graph.
func New(g *model.Graph) *Analyzer {
	a := &Analyzer{g: g, info: g.TypesInfo(), fset: g.FileSet(), untracked: map[*types.Var]string{}, assignedIn: map[ast.Node]map[*types.Var]bool{}, calls: map[ast.Node]bool{}}
	if a.info == nil || g.Func() == nil || g.Func().Body == nil {
		a.off = "the code is not available"
		return a
	}
	a.scan(g.Func())
	return a
}

// Off returns why the engine is switched off for this function, or "".
func (a *Analyzer) Off() string { return a.off }

// rootVar is the variable at the bottom of an addressable expression:
// x, x.f, x.f.g, x[i], (x), *x ... (nil when there is none).
func (a *Analyzer) rootVar(e ast.Expr) *types.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if sel := a.info.Selections[x]; sel == nil {
				// pkg.Name: a package-level variable, never tracked.
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.Ident:
			v, _ := a.info.ObjectOf(x).(*types.Var)
			return v
		default:
			return nil
		}
	}
}

func (a *Analyzer) markUntracked(e ast.Expr, why string) {
	if v := a.rootVar(e); v != nil {
		if _, seen := a.untracked[v]; !seen {
			a.untracked[v] = why
		}
	}
}

// scan finds everything that makes the function unsuitable, and every
// variable that must never be tracked (the safety list).
func (a *Analyzer) scan(fn *ast.FuncDecl) {
	var lits []*ast.FuncLit // the function literals around the node being visited
	var visit func(n ast.Node) bool
	// writtenInClosure marks v untracked when the write happens inside a
	// function literal that v is declared outside of.
	writtenInClosure := func(lhs ast.Expr) {
		if len(lits) == 0 {
			return
		}
		v := a.rootVar(lhs)
		if v == nil {
			return
		}
		inner := lits[len(lits)-1]
		if v.Pos() < inner.Pos() || v.Pos() >= inner.End() { // declared outside the closure
			if _, seen := a.untracked[v]; !seen {
				a.untracked[v] = "it is written inside a closure (a function literal)"
			}
		}
	}
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			lits = append(lits, x)
			ast.Inspect(x.Body, visit)
			lits = lits[:len(lits)-1]
			return false
		case *ast.BranchStmt:
			if x.Tok == token.GOTO {
				a.off = "it uses goto"
			}
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				if b, ok := a.info.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
					a.off = "it calls recover"
				}
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				if pn, ok := a.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "unsafe" {
					a.off = "it uses the unsafe package"
				}
			}
			// A method with a pointer receiver called on a value takes its
			// address without writing &.
			if sel := a.info.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
				if sig, ok := sel.Obj().Type().(*types.Signature); ok && sig.Recv() != nil {
					if _, ptrRecv := sig.Recv().Type().Underlying().(*types.Pointer); ptrRecv {
						if tv, ok := a.info.Types[x.X]; ok {
							if _, isPtr := tv.Type.Underlying().(*types.Pointer); !isPtr {
								a.markUntracked(x.X, "a method with a pointer receiver is called on it (Go takes its address)")
							}
						}
					}
				}
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				a.markUntracked(x.X, "its address is taken (&)")
			}
		case *ast.SliceExpr:
			if tv, ok := a.info.Types[x.X]; ok {
				if _, isArr := tv.Type.Underlying().(*types.Array); isArr {
					a.markUntracked(x.X, "a slice is taken of it (Go takes its address)")
				}
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				writtenInClosure(l)
			}
		case *ast.IncDecStmt:
			writtenInClosure(x.X)
		case *ast.RangeStmt:
			if x.Key != nil {
				writtenInClosure(x.Key)
			}
			if x.Value != nil {
				writtenInClosure(x.Value)
			}
		}
		return true
	}
	ast.Inspect(fn.Body, visit)
}

// trackable reports whether v may carry facts: a local or parameter of the
// workflow function that none of the safety-list rules excludes.
func (a *Analyzer) trackable(v *types.Var) bool {
	if v == nil || v.IsField() || v.Pkg() == nil {
		return false
	}
	if v.Parent() == v.Pkg().Scope() { // package-level
		return false
	}
	_, bad := a.untracked[v]
	return !bad
}

// assignedVars returns the variables assigned anywhere inside stmt
// (including inside closures and nested statements).
func (a *Analyzer) assignedVars(stmt ast.Node) map[*types.Var]bool {
	if m, ok := a.assignedIn[stmt]; ok {
		return m
	}
	m := map[*types.Var]bool{}
	add := func(e ast.Expr) {
		if e == nil {
			return
		}
		if v := a.rootVar(e); v != nil {
			m[v] = true
		}
	}
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				add(l)
			}
		case *ast.IncDecStmt:
			add(x.X)
		case *ast.RangeStmt:
			add(x.Key)
			add(x.Value)
		case *ast.ValueSpec:
			for _, name := range x.Names {
				if v, ok := a.info.Defs[name].(*types.Var); ok {
					m[v] = true
				}
			}
		}
		return true
	})
	a.assignedIn[stmt] = m
	return m
}

func (a *Analyzer) baseFile(pos token.Pos) (string, int) {
	p := a.fset.Position(pos)
	return filepath.Base(p.Filename), p.Line
}
