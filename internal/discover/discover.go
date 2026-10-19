// Package discover finds the workflow functions in loaded packages.
//
// Rule (docs/DESIGN.md D3): a top-level function or method
// is a workflow when its first parameter's type is
// go.temporal.io/sdk/workflow.Context, its last result is error, and it is
// exported. RegisterWorkflow calls are not looked at.
package discover

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// WorkflowPkgPath is the import path of Temporal's workflow package.
const WorkflowPkgPath = "go.temporal.io/sdk/workflow"

// Workflow is one discovered workflow function.
type Workflow struct {
	Name     string // "orders.OrderWorkflow" or "orders.Service.Run"
	Func     *ast.FuncDecl
	File     *ast.File
	Filename string
	Pkg      *packages.Package
	// Role is "" for a workflow, or what a small workflow made of a handler's
	// code is ("update validator"): set by model.FindHandlers, never by Find.
	Role string
}

// Namer decides the names of functions. Two functions of different
// packages that would share a name are told apart with the tail of their
// import path (see nameTable); which pairs share a name depends on which
// packages are looked at, so a command that needs names to stay the same
// whichever folder it is given builds one Namer from the whole set of
// packages it works with (the config's packages) and uses it for every
// load.
type Namer struct{ t nameTable }

// NewNamer builds a Namer from universe: every package whose functions
// might be compared by name.
func NewNamer(universe []*packages.Package) *Namer { return &Namer{uniqueNames(universe)} }

// Find returns every workflow in pkgs, sorted by name.
func Find(pkgs []*packages.Package) []Workflow { return FindWith(NewNamer(pkgs), pkgs) }

// FindWith is Find with the names decided by n.
func FindWith(n *Namer, pkgs []*packages.Package) []Workflow {
	var out []Workflow
	names := n.t
	for _, p := range pkgs {
		ctxType := workflowContextType(p.Types)
		if ctxType == nil {
			continue // the package doesn't import the workflow package
		}
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				obj, ok := p.TypesInfo.Defs[fn.Name].(*types.Func)
				if !ok || !isWorkflowSignature(obj.Type().(*types.Signature), ctxType) {
					continue
				}
				out = append(out, Workflow{
					Name:     names.of(p, fn),
					Func:     fn,
					File:     f,
					Filename: p.Fset.Position(f.Pos()).Filename,
					Pkg:      p,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func workflowContextType(pkg *types.Package) types.Type {
	if pkg == nil {
		return nil
	}
	for _, imp := range pkg.Imports() {
		if imp.Path() == WorkflowPkgPath {
			if obj := imp.Scope().Lookup("Context"); obj != nil {
				return obj.Type()
			}
		}
	}
	return nil
}

var errorType = types.Universe.Lookup("error").Type()

func isWorkflowSignature(sig *types.Signature, ctxType types.Type) bool {
	params, results := sig.Params(), sig.Results()
	if params.Len() == 0 || results.Len() == 0 {
		return false
	}
	return types.Identical(params.At(0).Type(), ctxType) &&
		types.Identical(results.At(results.Len()-1).Type(), errorType)
}

func name(pkgName string, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return pkgName + "." + fn.Name.Name
	}
	return pkgName + "." + receiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	case *ast.IndexListExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}

// Function is one top-level function or method, for looking up the names
// in a scope config (docs/DESIGN.md D8).
type Function struct {
	Workflow Workflow // the function, described like a discovered workflow
	// Auto: the automatic rule (Find) finds it as a workflow.
	Auto bool
	// TakesContext: its first parameter is workflow.Context, which D8
	// requires of anything added with "include".
	TakesContext bool
}

// All returns every top-level function and method in pkgs, sorted by name.
func All(pkgs []*packages.Package) []Function { return AllWith(NewNamer(pkgs), pkgs) }

// AllWith is All with the names decided by n.
func AllWith(n *Namer, pkgs []*packages.Package) []Function {
	var out []Function
	names := n.t
	for _, p := range pkgs {
		ctxType := workflowContextType(p.Types)
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, ok := p.TypesInfo.Defs[fn.Name].(*types.Func)
				if !ok {
					continue
				}
				sig := obj.Type().(*types.Signature)
				takesCtx := ctxType != nil && sig.Params().Len() > 0 && types.Identical(sig.Params().At(0).Type(), ctxType)
				out = append(out, Function{
					Workflow: Workflow{
						Name:     names.of(p, fn),
						Func:     fn,
						File:     f,
						Filename: p.Fset.Position(f.Pos()).Filename,
						Pkg:      p,
					},
					Auto:         takesCtx && fn.Name.IsExported() && isWorkflowSignature(sig, ctxType),
					TakesContext: takesCtx,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workflow.Name < out[j].Workflow.Name })
	return out
}

// nameTable gives every top-level function its name. Normally that is
// "pkg.Func" (or "pkg.Type.Method"). When two functions of different
// packages would get the same name (a/dup.Work and b/dup.Work are both
// "dup.Work"), they are told apart with the shortest unique tail of their
// import path: "a/dup.Work" and "b/dup.Work". Every table key (traces,
// overrides, stable ids, scope) is a name, so names must be unique, or two
// workflows would share each other's traces and overrides.
type nameTable map[nameKey]string

type nameKey struct {
	pkgPath string
	plain   string // "pkg.Func" or "pkg.Type.Method"
}

func (t nameTable) of(p *packages.Package, fn *ast.FuncDecl) string {
	plain := name(p.Name, fn)
	if q, ok := t[nameKey{p.PkgPath, plain}]; ok {
		return q
	}
	return plain
}

func uniqueNames(pkgs []*packages.Package) nameTable {
	byPlain := map[string]map[string]bool{} // plain name -> import paths using it
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					plain := name(p.Name, fn)
					if byPlain[plain] == nil {
						byPlain[plain] = map[string]bool{}
					}
					byPlain[plain][p.PkgPath] = true
				}
			}
		}
	}
	t := nameTable{}
	for plain, paths := range byPlain {
		if len(paths) < 2 {
			continue
		}
		var list []string
		for path := range paths {
			list = append(list, path)
		}
		sort.Strings(list)
		rest := plain[strings.Index(plain, ".")+1:] // "Func" or "Type.Method"
		for _, path := range list {
			t[nameKey{path, plain}] = shortestTail(path, list) + "." + rest
		}
	}
	return t
}

// shortestTail returns the fewest trailing "/"-separated segments of path
// that no other path in all ends with, at least two.
func shortestTail(path string, all []string) string {
	segs := strings.Split(path, "/")
	for k := 2; k <= len(segs); k++ {
		tail := strings.Join(segs[len(segs)-k:], "/")
		unique := true
		for _, other := range all {
			if other != path && (other == tail || strings.HasSuffix(other, "/"+tail)) {
				unique = false
				break
			}
		}
		if unique {
			return tail
		}
	}
	return path
}
