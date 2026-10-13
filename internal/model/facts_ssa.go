package model

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// SSA is the compiler's "single assignment" form of a function: every value
// has one name and never changes. Two decisions that read the same SSA
// value read the same fact, with nothing able to change it in between.
// That is exactly the proof the data-flow detectors need, so
// PathKit does not write its own "was it changed?" analysis.
//
// Everything here only reads: it builds SSA from the loaded syntax and types
// and never touches the AST, the graph, or any ID.

var (
	ssaMu    sync.Mutex
	ssaCache = map[*packages.Package]*ssaBuild{}
)

type ssaBuild struct {
	prog *ssa.Program
	pkg  *ssa.Package
}

// ssaFunction returns the SSA form of the workflow function, or nil when it
// cannot be built (a panic in the builder, an ill-typed package, ...). nil
// means "make no claims", never an error.
func (b *builder) ssaFunction() (fn *ssa.Function) {
	defer func() {
		if recover() != nil {
			fn = nil
		}
	}()
	p := b.wf.Pkg
	ssaMu.Lock()
	sb, ok := ssaCache[p]
	if !ok {
		prog, pkgs := ssautil.Packages([]*packages.Package{p}, ssa.InstantiateGenerics|ssa.GlobalDebug)
		if len(pkgs) == 1 && pkgs[0] != nil {
			pkgs[0].Build()
			sb = &ssaBuild{prog: prog, pkg: pkgs[0]}
		}
		ssaCache[p] = sb
	}
	ssaMu.Unlock()
	if sb == nil {
		return nil
	}
	obj, _ := b.info.Defs[b.wf.Func.Name].(*types.Func)
	if obj == nil {
		return nil
	}
	return sb.prog.FuncValue(obj)
}

// loopAncestors returns the if and range statements that sit inside
// another loop (a for or range, also through a function literal). Two
// decisions inside a loop can see different trips, so "same value" is not
// claimed for them.
func (b *builder) insideLoop() map[ast.Node]bool {
	out := map[ast.Node]bool{}
	var walk func(n ast.Node, in bool)
	walk = func(n ast.Node, in bool) {
		ast.Inspect(n, func(c ast.Node) bool {
			if c == nil || c == n {
				return c == n
			}
			switch c.(type) {
			case *ast.IfStmt, *ast.RangeStmt:
				if in {
					out[c] = true
				}
			}
			switch c := c.(type) {
			case *ast.ForStmt:
				walk(c.Body, true)
				return false
			case *ast.RangeStmt:
				walk(c.Body, true)
				return false
			}
			return true
		})
	}
	walk(b.wf.Func.Body, false)
	return out
}

// valueKey names the value v so equal keys mean the same value at run time:
// constants, parameters and pure operations on them compare by structure;
// a read of memory nobody ever writes compares by where it reads; anything
// else (a call, a phi, a load that could change) compares by identity, so
// two different ones are never equal.
func valueKey(v ssa.Value) string {
	switch v := v.(type) {
	case *ssa.Const:
		// Exact value and type. (Const.String abbreviates a long string
		// and prints a float with six digits, so two different constants
		// could share a key.)
		if v.Value == nil {
			return "const:nil:" + v.Type().String()
		}
		return "const:" + v.Value.ExactString() + ":" + v.Type().String()
	case *ssa.Parameter:
		return fmt.Sprintf("param:%p", v)
	case *ssa.BinOp:
		return "(" + valueKey(v.X) + v.Op.String() + valueKey(v.Y) + ")"
	case *ssa.UnOp:
		switch v.Op {
		case token.MUL:
			if a, idx, ok := readOnlyAddr(v.X); ok {
				return fmt.Sprintf("mem:%p/%d", a, idx)
			}
		case token.NOT, token.SUB, token.XOR:
			return v.Op.String() + valueKey(v.X)
		}
		// a load that could change, or a channel receive (<-ch)
	case *ssa.Field:
		return fmt.Sprintf("field%d(%s)", v.Field, valueKey(v.X))
	case *ssa.Call:
		if bi, ok := v.Call.Value.(*ssa.Builtin); ok && (bi.Name() == "len" || bi.Name() == "cap") && len(v.Call.Args) == 1 && fixedLength(v.Call.Args[0].Type()) {
			return bi.Name() + "(" + valueKey(v.Call.Args[0]) + ")"
		}
	}
	return fmt.Sprintf("id:%p", v)
}

// readOnlyAddr: the address is a local variable (or one of its fields)
// whose address never leaves the function and which is stored to only
// once, at its start (the copy of a parameter). Its value is then fixed.
func readOnlyAddr(addr ssa.Value) (*ssa.Alloc, int, bool) {
	idx := -1
	if fa, ok := addr.(*ssa.FieldAddr); ok {
		idx, addr = fa.Field, fa.X
	}
	a, ok := addr.(*ssa.Alloc)
	if !ok {
		return nil, 0, false
	}
	// The one store must be the copy of a parameter, made at the start of
	// the function before anything reads the variable. A variable that is
	// zero at first and assigned once later (var s T; ...read...; s = x)
	// has no store at its start, so its early reads see the zero value.
	var store *ssa.Store
	stores := 0
	var loads []ssa.Instruction
	for _, r := range *a.Referrers() {
		switch r := r.(type) {
		case *ssa.Store:
			if r.Addr != ssa.Value(a) || r.Val == ssa.Value(a) {
				return nil, 0, false
			}
			stores++
			store = r
		case *ssa.FieldAddr:
			for _, rr := range *r.Referrers() {
				switch rr := rr.(type) {
				case *ssa.UnOp:
					if rr.Op != token.MUL {
						return nil, 0, false
					}
					loads = append(loads, rr)
				case *ssa.DebugRef:
				default:
					return nil, 0, false // stored to, or its address passed on
				}
			}
		case *ssa.UnOp:
			if r.Op != token.MUL {
				return nil, 0, false
			}
			loads = append(loads, r)
		case *ssa.DebugRef:
		default:
			return nil, 0, false
		}
	}
	if stores != 1 {
		return nil, 0, false
	}
	if _, isParam := store.Val.(*ssa.Parameter); !isParam {
		return nil, 0, false
	}
	entry := a.Parent().Blocks[0]
	if store.Block() != entry {
		return nil, 0, false
	}
	at := indexIn(store)
	for _, l := range loads {
		if l.Block() == entry && indexIn(l) < at {
			return nil, 0, false
		}
	}
	return a, idx, true
}

// indexIn is the position of an instruction in its block.
func indexIn(in ssa.Instruction) int {
	for i, x := range in.Block().Instrs {
		if x == in {
			return i
		}
	}
	return -1
}

// fixedLength: the length of a value of this type cannot change once the
// value exists (a slice header, an array, a string, a pointer to an array).
// A map or a channel can grow or shrink under the same value.
func fixedLength(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Pointer:
		_, ok := u.Elem().Underlying().(*types.Array)
		return ok
	}
	return false
}

// groupKeys keeps only the keys that two or more junctions share.
func groupKeys(keys map[*Junction]string) map[string]int {
	count := map[string]int{}
	for _, k := range keys {
		count[k]++
	}
	return count
}
