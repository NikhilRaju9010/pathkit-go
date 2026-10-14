package model

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// A channel from workflow.GetSignalChannel is a ReceiveChannel, which has
// no Close. But the value behind it also is a Channel, so code can turn
// ANOTHER handle of the same named channel into a Channel and close it:
//
//	workflow.GetSignalChannel(ctx, "s").(workflow.Channel).Close()
//
// Then a receive on any handle of "s" returns more == false (verified in
// the test environment). The closed-signal-channel detector must not call
// that side impossible in a workflow where such a thing can happen.
//
// signalCloseTaint scans the whole package of the workflow (a helper may
// do the closing) for the two ways a signal channel can be turned into
// something that can be closed or passed beyond view:
//   - a type assertion whose operand or target type is an SDK channel
//     interface (workflow.ReceiveChannel, workflow.Channel, ...);
//   - a call that passes a value of such a type to a function of another
//     package (that is not the SDK), or to a function value PathKit cannot
//     identify.
//
// When the channel's name can be read (the assertion is applied to a
// GetSignalChannel call, or to a variable set from one, with a constant
// name), only that name is tainted; otherwise every signal channel is.
// What a function of ANOTHER package does with a channel handed to it is
// still assumed (the detector's definition says so).
type closeTaint struct {
	all   bool
	names map[string]bool
}

var (
	taintMu    sync.Mutex
	taintCache = map[*packages.Package]*closeTaint{}
)

func (b *builder) signalCloseTaint() *closeTaint {
	taintMu.Lock()
	defer taintMu.Unlock()
	if t, ok := taintCache[b.wf.Pkg]; ok {
		return t
	}
	t := &closeTaint{names: map[string]bool{}}
	taintCache[b.wf.Pkg] = t
	for _, file := range b.wf.Pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			b.scanTaint(fd.Body, t)
		}
	}
	return t
}

func (b *builder) scanTaint(body *ast.BlockStmt, t *closeTaint) {
	taint := func(operand ast.Expr) {
		if name, ok := b.signalNameOf(operand, body); ok {
			t.names[name] = true
		} else {
			t.all = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeAssertExpr:
			if b.isSDKChannel(b.info.TypeOf(n.X)) || (n.Type != nil && b.isSDKChannel(b.info.TypeOf(n.Type))) {
				taint(n.X)
			}
		case *ast.CallExpr:
			f := b.calledFunc(n)
			if f != nil && f.Pkg() != nil && (strings.HasPrefix(f.Pkg().Path(), "go.temporal.io/sdk") || f.Pkg() == b.wf.Pkg.Types) {
				return true // the SDK itself, or this package (scanned here)
			}
			if _, isConv := b.info.Types[n.Fun]; isConv && b.info.Types[n.Fun].IsType() {
				return true // a conversion, not a call
			}
			if bi, ok := b.info.Uses[identOf(n.Fun)].(*types.Builtin); ok && bi != nil {
				return true
			}
			for _, arg := range n.Args {
				if b.isSDKChannel(b.info.TypeOf(arg)) {
					taint(arg)
				}
			}
		}
		return true
	})
}

func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	return id
}

// isSDKChannel: a named type of the Temporal SDK that is a channel
// interface.
func (b *builder) isSDKChannel(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok {
		if a, isAlias := t.(*types.Alias); isAlias {
			return b.isSDKChannel(types.Unalias(a))
		}
		return false
	}
	obj := n.Obj()
	if obj == nil || obj.Pkg() == nil || !strings.HasPrefix(obj.Pkg().Path(), "go.temporal.io/sdk") {
		return false
	}
	switch obj.Name() {
	case "ReceiveChannel", "Channel", "SendChannel":
		return true
	}
	return false
}

// signalNameOf reads the constant signal name an expression stands for:
// a GetSignalChannel call, or a variable defined once in body from one.
func (b *builder) signalNameOf(e ast.Expr, body *ast.BlockStmt) (string, bool) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.CallExpr:
		return b.signalCallName(x)
	case *ast.Ident:
		obj, _ := b.info.Uses[x].(*types.Var)
		if obj == nil {
			return "", false
		}
		// exactly one write, and it is a GetSignalChannel call with a
		// constant name (a declaration with no value is not a write)
		writes, name := 0, ""
		from := func(e ast.Expr) {
			writes++
			if c, ok := ast.Unparen(e).(*ast.CallExpr); ok {
				if nm, ok := b.signalCallName(c); ok {
					name = nm
					return
				}
			}
			name = ""
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for i, l := range n.Lhs {
					id, ok := ast.Unparen(l).(*ast.Ident)
					if !ok || (b.info.Defs[id] != obj && b.info.Uses[id] != obj) {
						continue
					}
					if len(n.Lhs) == len(n.Rhs) {
						from(n.Rhs[i])
					} else {
						writes += 2
					}
				}
			case *ast.ValueSpec:
				for i, id := range n.Names {
					if b.info.Defs[id] == obj && len(n.Values) == len(n.Names) {
						from(n.Values[i])
					} else if b.info.Defs[id] == obj && len(n.Values) != 0 {
						writes += 2
					}
				}
			case *ast.IncDecStmt:
				if id := identOf(n.X); id != nil && b.info.Uses[id] == obj {
					writes += 2
				}
			case *ast.RangeStmt:
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if id := identOf(e); id != nil && (b.info.Uses[id] == obj || b.info.Defs[id] == obj) {
						writes += 2
					}
				}
			}
			return true
		})
		if writes == 1 && name != "" {
			return name, true
		}
	}
	return "", false
}

func (b *builder) signalCallName(c *ast.CallExpr) (string, bool) {
	f := b.calledFunc(c)
	if !isPkgFunc(f, workflowPkg, "GetSignalChannel", "GetSignalChannelWithOptions") || len(c.Args) < 2 {
		return "", false
	}
	tv, ok := b.info.Types[c.Args[1]]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
