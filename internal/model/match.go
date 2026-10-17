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
	return hashText(strings.Join(toks, " "))
}

// hashText is the short hash FunctionHash and CodeHash are written in.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// Mismatch explains why a trace doesn't fit the graph, naming the step.
type Mismatch struct {
	Step   int // 1-based step number, or 0 when the problem is at the end
	Reason string
	// Followed are the steps that did fit, in order (after loop folding),
	// and At is where on the map the walk stood when the trace left it:
	// what the unmatched-trace report shows ("followed the map ...; the
	// map expected ..."). They come from this one walk, never a second.
	Followed []Step
	At       Target
	// Bad is the exit the step that did not fit names (nil when the step
	// is not an exit of this workflow, or the trace simply stopped).
	Bad *Exit
}

func (m *Mismatch) Error() string { return m.Reason }

// Match walks the graph with a recorded trace, one step at a time (the
// matcher never compares two lists). It returns the path the trace took,
// or a Mismatch naming the exact step that left the graph.
//
// It also applies the matcher half of the loop rule (docs/DESIGN.md D3): when
// the trace goes into a loop's body again (another "iterate" of a loop it
// is already in), every step since that loop's previous "iterate" is
// dropped, including the steps of loops nested inside it, and the walk
// carries on from the new "iterate". Only the last trip through the body
// is kept. The number of trips is never counted.
func (g *Graph) Match(raw []string) (Path, *Mismatch) {
	steps, inSite, bad := g.splitMarkers(raw)
	if bad != "" {
		return Path{}, &Mismatch{Step: 0, Reason: bad, At: g.Start}
	}
	at := g.Start
	var path Path
	iterateAt := map[*Junction]int{} // loop -> index of its kept "iterate" step
	fail := func(n int, reason string) (Path, *Mismatch) {
		mm := &Mismatch{Step: n, Reason: reason, Followed: append([]Step(nil), path.Steps...), At: at}
		if n > 0 {
			if e, ok := g.LookupEdge(steps[n-1]); ok {
				mm.Bad = e
			}
		}
		return Path{}, mm
	}
	for i, s := range steps {
		n := i + 1
		if at.Dead() {
			return fail(n, fmt.Sprintf("step %d %q comes after the path already went into a panic", n, s))
		}
		if at.IsEnd() {
			return fail(n, fmt.Sprintf("step %d %q comes after the path already ended at %s", n, s, at.End))
		}
		exit, ok := g.LookupEdge(s)
		if !ok {
			return fail(n, fmt.Sprintf("step %d %q is not a junction exit in %s", n, s, g.Workflow))
		}
		if want := siteID(exit.Junction.Site); inSite[i] != want {
			return fail(n, fmt.Sprintf("step %d %q was recorded %s, but the map has %s %s", n, s, where(inSite[i]), exit.Junction.ID, where(want)))
		}
		if r := at.Retry; r != nil {
			if exit != r {
				return fail(n, fmt.Sprintf("step %d %q does not fit: the path goes round loop %s (%s) next, so %s comes first", n, s, r.Junction.ID, r.Junction.Label, r.ID))
			}
			path.Steps = append(path.Steps, Step{Junction: r.Junction, Exit: r})
			at = r.To
			continue
		}
		// A retry is never chosen at a junction: only the end of a loop's
		// body leads to it (handled above).
		if exit.Junction != at.Junction || exit == exit.Junction.Retry {
			return fail(n, fmt.Sprintf("step %d %q does not fit: the path is at %s (%s)", n, s, at.Junction.ID, at.Junction.Label))
		}
		if at.Only != nil && exit != at.Only {
			return fail(n, fmt.Sprintf("step %d %q does not fit: at %s (%s) the code before it already decided, and the map expects %s", n, s, at.Junction.ID, at.Junction.Label, at.Only.ID))
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
		return fail(0, "the trace ends in a panic, not at an end")
	case at.Retry != nil:
		return fail(0, fmt.Sprintf("the trace stops before %s (going round loop %s) and never reaches an end", at.Retry.ID, at.Retry.Junction.ID))
	case at.Junction != nil:
		return fail(0, fmt.Sprintf("the trace stops at %s (%s) before reaching an end", at.Junction.ID, at.Junction.Label))
	}
	path.End = at.End
	path.Compensation = g.compensationOn(path.Steps)
	return path, nil
}

// siteID is the ID of a site, "" for the workflow function.
func siteID(s *Site) string {
	if s == nil {
		return ""
	}
	return s.ID
}

func where(site string) string {
	if site == "" {
		return "in the workflow function"
	}
	return "in helper copy " + site
}

// splitMarkers takes the enter and exit markers (">S1", "<S1") out of a
// recorded trace. It returns the steps without them, the site each step was
// recorded in, and a reason when the markers do not fit each other or the
// graph (a helper left that was not entered, or never left).
func (g *Graph) splitMarkers(raw []string) (steps, inSite []string, bad string) {
	var stack []string
	known := map[string]bool{}
	for _, s := range g.sites {
		known[s.ID] = true
	}
	for _, s := range raw {
		switch {
		case len(s) > 1 && s[0] == '>':
			if !known[s[1:]] {
				return nil, nil, fmt.Sprintf("the trace enters %q, a copy of helper code that is not in %s", s[1:], g.Workflow)
			}
			stack = append(stack, s[1:])
		case len(s) > 1 && s[0] == '<':
			if len(stack) == 0 || stack[len(stack)-1] != s[1:] {
				return nil, nil, fmt.Sprintf("the trace leaves %q, which it is not in", s[1:])
			}
			stack = stack[:len(stack)-1]
		default:
			top := ""
			if len(stack) > 0 {
				top = stack[len(stack)-1]
			}
			steps = append(steps, s)
			inSite = append(inSite, top)
		}
	}
	if len(stack) > 0 {
		return nil, nil, fmt.Sprintf("the trace enters %q and never leaves it", stack[len(stack)-1])
	}
	return steps, inSite, ""
}
