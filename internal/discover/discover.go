// Package discover finds the workflow functions in loaded packages.
//
// Rule (CLAUDE.md D3, approved 2026-09-28): a top-level function or method
// is a workflow when its first parameter's type is
// go.temporal.io/sdk/workflow.Context, its last result is error, and it is
// exported. RegisterWorkflow calls are not looked at.
package discover

import (
	"go/ast"
	"go/types"
	"sort"

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
}

// Find returns every workflow in pkgs, sorted by name.
func Find(pkgs []*packages.Package) []Workflow {
	var out []Workflow
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
					Name:     name(p.Name, fn),
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
// in a scope config (CLAUDE.md D8).
type Function struct {
	Workflow Workflow // the function, described like a discovered workflow
	// Auto: the automatic rule (Find) finds it as a workflow.
	Auto bool
	// TakesContext: its first parameter is workflow.Context, which D8
	// requires of anything added with "include".
	TakesContext bool
}

// All returns every top-level function and method in pkgs, sorted by name.
func All(pkgs []*packages.Package) []Function {
	var out []Function
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
						Name:     name(p.Name, fn),
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
