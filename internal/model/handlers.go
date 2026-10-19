package model

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
)

// The kinds of handler a workflow registers.
const (
	HandlerSignal    = "signal"
	HandlerQuery     = "query"
	HandlerUpdate    = "update"
	HandlerValidator = "update validator"
)

// A Handler is something a workflow registers to be called from outside:
// a signal channel it reads, a query handler, an update handler, or an update
// validator. The code of an update handler or validator is analysed like a small
// workflow of its own (paths and branches); a query handler or a signal has no
// map of its own.
type Handler struct {
	// Owner is the workflow that registers it.
	Owner string
	Kind  string
	// Name is the signal, query or update name; "" when it is not a constant
	// text (the table then says "(computed)").
	Name string
	// Pos is where it is registered.
	Pos token.Position
	// Workflow is the small workflow made of the handler's code, or nil when
	// there is none (a signal, a query, code in another package, a function
	// value PathKit cannot trace to one function). The name of a handler whose
	// function is itself a workflow PathKit finds is in WorkflowName instead,
	// with Workflow nil: it is not analysed twice.
	Workflow *discover.Workflow
	// WorkflowName is the name the handler's small workflow has in reports and
	// traces ("orders.Run.update[add]"), or the discovered workflow's name.
	WorkflowName string
	// Why says in a line why there is no map of the handler's code.
	Why string
}

// FindHandlers lists what the workflow registers: in its own code and in the
// helpers of its package it calls (up to maxHelperDepth deep). known maps a
// function declaration PathKit already finds as a workflow to its name, so
// that such a handler is not made a second workflow.
func FindHandlers(wf discover.Workflow, known map[*ast.FuncDecl]string) []Handler {
	b := newBuilder(wf)
	var out []Handler
	seenBody := map[*ast.BlockStmt]bool{}
	var scan func(body *ast.BlockStmt, depth int)
	scan = func(body *ast.BlockStmt, depth int) {
		if seenBody[body] {
			return
		}
		seenBody[body] = true
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			f := b.calledFunc(call)
			switch {
			case isPkgFunc(f, workflowPkg, "GetSignalChannel") && len(call.Args) == 2:
				out = append(out, b.handlerAt(wf, HandlerSignal, call, call.Args[1], nil, known))
			case isPkgFunc(f, workflowPkg, "SetQueryHandler") && len(call.Args) == 3:
				out = append(out, b.handlerAt(wf, HandlerQuery, call, call.Args[1], call.Args[2], known))
			case isPkgFunc(f, workflowPkg, "SetUpdateHandler") && len(call.Args) == 3:
				out = append(out, b.handlerAt(wf, HandlerUpdate, call, call.Args[1], call.Args[2], known))
			case isPkgFunc(f, workflowPkg, "SetUpdateHandlerWithOptions") && len(call.Args) == 4:
				out = append(out, b.handlerAt(wf, HandlerUpdate, call, call.Args[1], call.Args[2], known))
				if v := validatorOf(call.Args[3]); v != nil {
					out = append(out, b.handlerAt(wf, HandlerValidator, call, call.Args[1], v, known))
				}
			default:
				if depth < maxHelperDepth {
					if h := b.calleeOf(call); h != nil && h.body != nil && b.takesContext(call) {
						scan(h.body, depth+1)
					}
				}
			}
			return true
		})
	}
	scan(wf.Func.Body, 0)
	sort.SliceStable(out, func(i, k int) bool {
		a, c := out[i].Pos, out[k].Pos
		if a.Filename != c.Filename {
			return a.Filename < c.Filename
		}
		if a.Line != c.Line {
			return a.Line < c.Line
		}
		return out[i].Kind < out[k].Kind
	})
	return out
}

// validatorOf finds the Validator in a composite literal of UpdateHandlerOptions.
func validatorOf(opts ast.Expr) ast.Expr {
	lit, ok := ast.Unparen(opts).(*ast.CompositeLit)
	if !ok {
		return nil
	}
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Validator" {
				return kv.Value
			}
		}
	}
	return nil
}

func (b *builder) handlerAt(wf discover.Workflow, kind string, call *ast.CallExpr, nameArg, fn ast.Expr, known map[*ast.FuncDecl]string) Handler {
	h := Handler{Owner: wf.Name, Kind: kind, Pos: b.fset.Position(call.Pos())}
	if tv, ok := b.info.Types[nameArg]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		h.Name = constant.StringVal(tv.Value)
	}
	if fn == nil {
		h.Why = "a " + kind + " has no code of its own to map"
		return h
	}
	if kind == HandlerQuery {
		h.Why = "a query handler takes no workflow.Context, so a run cannot be recorded in it"
		return h
	}
	decl, why := b.handlerFunc(fn)
	if decl == nil {
		h.Why = why
		return h
	}
	if name, ok := known[decl]; ok {
		h.WorkflowName = name
		h.Why = "analysed as the workflow " + name
		return h
	}
	if len(decl.Type.Params.List) == 0 || decl.Body == nil {
		h.Why = "it has no parameters to take the workflow context from"
		return h
	}
	if first := decl.Type.Params.List[0]; len(first.Names) == 0 || !isWorkflowContext(b.info.TypeOf(first.Type)) {
		h.Why = "its first parameter is not a named workflow.Context"
		return h
	}
	short := "update"
	if kind == HandlerValidator {
		short = "validator"
	}
	label := h.Name
	if label == "" {
		label = fmt.Sprintf("L%d", h.Pos.Line)
	}
	h.WorkflowName = fmt.Sprintf("%s.%s[%s]", wf.Name, short, label)
	h.Workflow = &discover.Workflow{Name: h.WorkflowName, Func: decl, File: fileOf(wf, decl, b), Filename: b.fset.Position(decl.Pos()).Filename, Pkg: wf.Pkg}
	if kind == HandlerValidator {
		h.Workflow.Role = HandlerValidator
	}
	return h
}

// fileOf finds the syntax file that holds decl.
func fileOf(wf discover.Workflow, decl *ast.FuncDecl, b *builder) *ast.File {
	for _, f := range wf.Pkg.Syntax {
		if f.Pos() <= decl.Pos() && decl.End() <= f.End() {
			return f
		}
	}
	return wf.File
}

// handlerFunc finds the function a handler argument stands for: a function
// literal (given a declaration of its own so it can be mapped like a workflow
// function), or a function or method of the package.
func (b *builder) handlerFunc(e ast.Expr) (*ast.FuncDecl, string) {
	switch x := ast.Unparen(e).(type) {
	case *ast.FuncLit:
		return &ast.FuncDecl{Name: ast.NewIdent("handler"), Type: x.Type, Body: x.Body}, ""
	case *ast.Ident, *ast.SelectorExpr:
		var id *ast.Ident
		if s, ok := x.(*ast.SelectorExpr); ok {
			id = s.Sel
		} else {
			id = x.(*ast.Ident)
		}
		fn, _ := b.info.Uses[id].(*types.Func)
		if fn == nil {
			return nil, "it is a function value PathKit cannot trace to one function"
		}
		if h := b.helperDecls()[fn]; h != nil && h.decl != nil {
			return h.decl, ""
		}
		return nil, "its code is in another package"
	}
	return nil, "it is not a function literal or a named function"
}
