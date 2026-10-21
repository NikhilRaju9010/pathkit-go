package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Cleanup after a cancel (milestone M11, O2). Code that runs under a context
// made by workflow.NewDisconnectedContext is the cleanup a workflow does after
// it was cancelled (or failed): it survives the cancellation. The decisions in
// it are marked, so a report can show them as a section of their own. The paths
// and their stable IDs are exactly what they were: this is a mark on a
// decision, never part of an ID.

type cleanupRegion struct{ from, to token.Pos }

// InCleanup reports whether a decision of the workflow function runs under a
// disconnected context: it checks a call made with that context, or it sits
// after the statement that made the context, in the same block.
func (g *Graph) InCleanup(j *Junction) bool {
	if j.Site != nil || g.fn == nil || g.fn.Body == nil || g.info == nil {
		return false
	}
	if g.cleanupObjs == nil {
		g.findCleanup()
	}
	if c := j.callExpr; c != nil && len(c.Args) > 0 {
		if id, ok := ast.Unparen(c.Args[0]).(*ast.Ident); ok && g.cleanupObjs[g.info.Uses[id]] {
			return true
		}
	}
	p := j.Stmt.Pos()
	for _, r := range g.cleanupRegions {
		if p >= r.from && p < r.to {
			return true
		}
	}
	return false
}

func (g *Graph) findCleanup() {
	g.cleanupObjs = map[types.Object]bool{}
	walk := func(n ast.Node) {
		ast.Inspect(n, func(m ast.Node) bool {
			blk, ok := m.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for _, st := range blk.List {
				as, ok := st.(*ast.AssignStmt)
				if !ok || len(as.Rhs) != 1 {
					continue
				}
				call, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
				if !ok {
					continue
				}
				var fn *types.Func
				switch f := ast.Unparen(call.Fun).(type) {
				case *ast.SelectorExpr:
					fn, _ = g.info.Uses[f.Sel].(*types.Func)
				case *ast.Ident:
					fn, _ = g.info.Uses[f].(*types.Func)
				}
				if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != workflowPkg || fn.Name() != "NewDisconnectedContext" {
					continue
				}
				if id, ok := as.Lhs[0].(*ast.Ident); ok {
					if o := g.info.ObjectOf(id); o != nil {
						g.cleanupObjs[o] = true
					}
				}
				g.cleanupRegions = append(g.cleanupRegions, cleanupRegion{from: st.End(), to: blk.End()})
			}
			return true
		})
	}
	walk(g.fn.Body)
}
