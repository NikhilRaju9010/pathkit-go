package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/printer"
	"go/scanner"
	"go/token"
	"strings"
)

// FunctionHash fingerprints a workflow function's code, ignoring comments
// and formatting: the function is printed without its doc comment, split
// into Go tokens (comments dropped, line ends treated as ";"), and hashed.
// A trace recorded with a different hash is stale. This is the only place
// the hash is computed.
func FunctionHash(fset *token.FileSet, fn *ast.FuncDecl) string {
	decl := *fn
	decl.Doc = nil
	var src bytes.Buffer
	if err := printer.Fprint(&src, fset, &decl); err != nil {
		return ""
	}

	var s scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, src.Len())
	s.Init(file, src.Bytes(), nil, 0) // mode 0: comments are skipped
	var toks []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON {
			lit = ";"
		}
		if lit == "" {
			lit = tok.String()
		}
		toks = append(toks, lit)
	}
	sum := sha256.Sum256([]byte(strings.Join(toks, " ")))
	return hex.EncodeToString(sum[:])[:16]
}

// Mismatch explains why a trace doesn't fit the graph, naming the step.
type Mismatch struct {
	Step   int // 1-based step number, or 0 when the problem is at the end
	Reason string
}

func (m *Mismatch) Error() string { return m.Reason }

// Match walks the graph with a recorded trace, one step at a time (the
// matcher never compares two lists). It returns the path the trace took,
// or a Mismatch naming the exact step that left the graph.
func (g *Graph) Match(steps []string) (Path, *Mismatch) {
	at := g.Start
	var path Path
	for i, s := range steps {
		n := i + 1
		if at.Dead() {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q comes after the path already went into a panic", n, s)}
		}
		if at.Junction == nil {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q comes after the path already ended at %s", n, s, at.End)}
		}
		exit, ok := g.LookupEdge(s)
		if !ok {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q is not a junction exit in %s", n, s, g.Workflow)}
		}
		if exit.Junction != at.Junction {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q does not fit: the path is at %s (%s)", n, s, at.Junction.ID, at.Junction.Label)}
		}
		path.Steps = append(path.Steps, Step{Junction: at.Junction, Exit: exit})
		at = exit.To
	}
	switch {
	case at.Dead():
		return Path{}, &Mismatch{0, "the trace ends in a panic, not at an end"}
	case at.Junction != nil:
		return Path{}, &Mismatch{0, fmt.Sprintf("the trace stops at %s (%s) before reaching an end", at.Junction.ID, at.Junction.Label)}
	}
	path.End = at.End
	return path, nil
}
