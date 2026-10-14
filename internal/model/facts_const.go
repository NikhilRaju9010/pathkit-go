package model

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
)

// extractConst records, on every plain if whose condition the compiler
// knows the value of, the exit that can never be taken.
//
// "Known" is meant strictly, because a wrong answer here hides a real path:
//   - A const is constant. A package-level var is not, even if nothing
//     assigns it (a test, another file or -ldflags -X can).
//   - The value must not depend on the machine: no identifier from another
//     package (runtime.GOOS, unsafe.Sizeof, math.MaxInt, os.PathSeparator),
//     no shift and no ^ (their value depends on the size of int).
//   - A const from another package is not followed (its package is not
//     loaded for editing here); a const of this package is followed back
//     to its declaration and is trusted only if that declaration is also
//     trusted and its file has no build constraint (//go:build, or a
//     _linux / _amd64 style name), because another build would see another
//     value.
func extractConst(b *builder, g *Graph) {
	for _, j := range g.Junctions {
		ifs, ok := j.Stmt.(*ast.IfStmt)
		if !ok || j.Kind != PlainIf {
			continue
		}
		tv, ok := b.info.Types[ifs.Cond]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.Bool || !b.trustedConstant(ifs.Cond, map[types.Object]bool{}) {
			continue
		}
		never, value := "true", "false"
		if constant.BoolVal(tv.Value) {
			never, value = "false", "true"
		}
		j.Facts = append(j.Facts, Fact{Kind: ConstCondition, Exit: never, Detail: "the condition is always " + value})
	}
}

// trustedConstant reports whether the value of expr is the same on every
// machine and in every build (see extractConst).
func (b *builder) trustedConstant(expr ast.Expr, seen map[types.Object]bool) bool {
	ok := true
	ast.Inspect(expr, func(n ast.Node) bool {
		if !ok {
			return false
		}
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ok = false // another package's name (or a field): not followed
		case *ast.BinaryExpr:
			if n.Op == token.SHL || n.Op == token.SHR {
				ok = false
			}
		case *ast.UnaryExpr:
			if n.Op == token.XOR {
				ok = false
			}
		case *ast.CallExpr:
			// Conversions and the builtins len/cap/min/max are fine; any
			// other call has no constant value, so cannot be here.
		case *ast.Ident:
			switch obj := b.info.Uses[n].(type) {
			case *types.Const:
				if obj.Pkg() == nil { // true, false, iota
					return true
				}
				if obj.Pkg() != b.wf.Pkg.Types || !b.trustedConstDecl(obj, seen) {
					ok = false
				}
			case *types.TypeName:
				// Only the predeclared types (int, string, ...). A type of this
				// package may hide a machine-dependent size, e.g.
				// type T [unsafe.Sizeof(uintptr(0))]byte.
				if obj.Pkg() != nil {
					ok = false
				}
			case *types.Builtin:
				if obj.Pkg() != nil { // unsafe.Sizeof through a dot import
					ok = false
				}
			case nil:
			default:
				ok = false // a variable, function or package name
			}
		}
		return ok
	})
	return ok
}

func (b *builder) trustedConstDecl(c *types.Const, seen map[types.Object]bool) bool {
	if seen[c] {
		return false
	}
	seen[c] = true
	file := b.fset.Position(c.Pos()).Filename
	if buildConstrained(b, file) {
		return false
	}
	for _, f := range b.wf.Pkg.Syntax {
		if b.fset.Position(f.Pos()).Filename != file {
			continue
		}
		var found bool
		trusted := true
		ast.Inspect(f, func(n ast.Node) bool {
			gd, ok := n.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST || found {
				return !found
			}
			var last []ast.Expr
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				if len(vs.Values) > 0 {
					last = vs.Values
				}
				for i, name := range vs.Names {
					if b.info.Defs[name] != c {
						continue
					}
					found = true
					if i < len(last) {
						trusted = b.trustedConstant(last[i], seen)
					} else {
						trusted = false
					}
				}
			}
			return false
		})
		return found && trusted
	}
	return false
}

// knownGOOS and knownGOARCH are the names Go treats as build constraints
// when they end a file name (name_linux.go, name_amd64.go, name_linux_arm64.go).
var knownGOOS = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos"}
var knownGOARCH = []string{"386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "loong64", "mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"}

// buildConstrained reports whether a file only exists in some builds: a
// //go:build (or // +build) line before the package clause, or a name
// ending in a known GOOS / GOARCH.
func buildConstrained(b *builder, file string) bool {
	base := strings.TrimSuffix(filepath.Base(file), ".go")
	base = strings.TrimSuffix(base, "_test")
	parts := strings.Split(base, "_")
	if n := len(parts); n >= 2 {
		last := parts[n-1]
		if slices.Contains(knownGOOS, last) || slices.Contains(knownGOARCH, last) {
			return true
		}
	}
	for _, f := range b.wf.Pkg.Syntax {
		if b.fset.Position(f.Pos()).Filename != file {
			continue
		}
		for _, cg := range f.Comments {
			if cg.Pos() >= f.Package {
				break
			}
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
					return true
				}
			}
		}
	}
	return false
}
