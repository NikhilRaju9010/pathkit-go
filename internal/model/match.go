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
//
// It also applies the matcher half of the loop rule (CLAUDE.md D3): when
// the trace goes into a loop's body again (another "iterate" of a loop it
// is already in), every step since that loop's previous "iterate" is
// dropped, including the steps of loops nested inside it, and the walk
// carries on from the new "iterate". Only the last trip through the body
// is kept. The number of trips is never counted.
func (g *Graph) Match(steps []string) (Path, *Mismatch) {
	at := g.Start
	var path Path
	iterateAt := map[*Junction]int{} // loop -> index of its kept "iterate" step
	for i, s := range steps {
		n := i + 1
		if at.Dead() {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q comes after the path already went into a panic", n, s)}
		}
		if at.IsEnd() {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q comes after the path already ended at %s", n, s, at.End)}
		}
		exit, ok := g.LookupEdge(s)
		if !ok {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q is not a junction exit in %s", n, s, g.Workflow)}
		}
		if r := at.Retry; r != nil {
			if exit != r {
				return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q does not fit: the path goes round loop %s (%s) next, so %s comes first", n, s, r.Junction.ID, r.Junction.Label, r.ID)}
			}
			path.Steps = append(path.Steps, Step{Junction: r.Junction, Exit: r})
			at = r.To
			continue
		}
		// A retry is never chosen at a junction: only the end of a loop's
		// body leads to it (handled above).
		if exit.Junction != at.Junction || exit == exit.Junction.Retry {
			return Path{}, &Mismatch{n, fmt.Sprintf("step %d %q does not fit: the path is at %s (%s)", n, s, at.Junction.ID, at.Junction.Label)}
		}
		if j := exit.Junction; j.Kind == Loop && exit.Label == "iterate" {
			if k, again := iterateAt[j]; again {
				path.Steps = path.Steps[:k] // keep only the last trip
				for loop, idx := range iterateAt {
					if idx >= k {
						delete(iterateAt, loop) // this loop and loops nested in it
					}
				}
			}
			iterateAt[j] = len(path.Steps)
		}
		path.Steps = append(path.Steps, Step{Junction: at.Junction, Exit: exit})
		at = exit.To
	}
	switch {
	case at.Dead():
		return Path{}, &Mismatch{0, "the trace ends in a panic, not at an end"}
	case at.Retry != nil:
		return Path{}, &Mismatch{0, fmt.Sprintf("the trace stops before %s (going round loop %s) and never reaches an end", at.Retry.ID, at.Retry.Junction.ID)}
	case at.Junction != nil:
		return Path{}, &Mismatch{0, fmt.Sprintf("the trace stops at %s (%s) before reaching an end", at.Junction.ID, at.Junction.Label)}
	}
	path.End = at.End
	return path, nil
}
