package load

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModuleRoot returns the folder with the go.mod that applies to path ("" when
// there is none).
func ModuleRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for d := abs; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, "go.mod")); err == nil && !info.IsDir() {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// CustomConverterUse lists (at most five) places in the module under root
// that set their own Temporal data converter or payload codec, as
// "file.go:12: the line". Empty means none found. Only code counts, not
// comments or text. What counts:
//   - a client or worker option named DataConverter (in a struct literal, or
//     assigned: opts.DataConverter = x);
//   - a call of converter.NewCodecDataConverter or NewCompositeDataConverter;
//   - a type with the methods of a data converter (ToPayloads and
//     FromPayloads), a payload converter (ToPayload and FromPayload) or a
//     payload codec (Encode and Decode taking payloads).
//
// json.Number and UseNumber are not here: they change only the values a
// decoder in that code produces, and the rule asks about the decoder that
// produced the value in question (internal/facts).
//
// Folders named vendor, testdata, node_modules, .git and .pathkit, symbolic
// links to folders, and files that cannot be read or parsed are skipped.
func CustomConverterUse(root string) []string {
	var hits []string
	methods := map[string]map[string]bool{} // folder + "\x00" + receiver type -> method names that look like a converter's
	firstAt := map[string]string{}          // the same key -> where the first of those methods is
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if len(hits) >= 5 {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", ".pathkit", "node_modules", "testdata":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		rel, _ := filepath.Rel(root, path)
		where := func(p token.Pos) string {
			n := fset.Position(p).Line
			return fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), n, strings.TrimSpace(lines[n-1]))
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && id.Name == "DataConverter" {
					hits = append(hits, where(x.Pos()))
				}
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "DataConverter" {
						hits = append(hits, where(x.Pos()))
					}
				}
			case *ast.SelectorExpr:
				if x.Sel.Name == "NewCodecDataConverter" || x.Sel.Name == "NewCompositeDataConverter" {
					hits = append(hits, where(x.Pos()))
				}
			case *ast.FuncDecl:
				if x.Recv == nil || len(x.Recv.List) == 0 {
					return true
				}
				name := converterMethod(x)
				if name == "" {
					return true
				}
				key := filepath.Dir(path) + "\x00" + recvName(x.Recv.List[0].Type)
				if methods[key] == nil {
					methods[key] = map[string]bool{}
					firstAt[key] = where(x.Pos())
				}
				methods[key][name] = true
			}
			return true
		})
		return nil
	})
	var keys []string
	for k := range methods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m := methods[k]
		if len(hits) < 5 && ((m["ToPayloads"] && m["FromPayloads"]) || (m["ToPayload"] && m["FromPayload"]) || (m["Encode"] && m["Decode"])) {
			hits = append(hits, firstAt[k])
		}
	}
	if len(hits) > 5 {
		hits = hits[:5]
	}
	return hits
}

// converterMethod returns the name of a method that looks like part of a data
// converter, payload converter or payload codec ("" for any other).
func converterMethod(fn *ast.FuncDecl) string {
	switch n := fn.Name.Name; n {
	case "ToPayloads", "FromPayloads", "ToPayload", "FromPayload":
		return n
	case "Encode", "Decode":
		if fn.Type.Params != nil {
			for _, p := range fn.Type.Params.List {
				if strings.Contains(types.ExprString(p.Type), "Payload") {
					return n
				}
			}
		}
	}
	return ""
}

func recvName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	return types.ExprString(e)
}
