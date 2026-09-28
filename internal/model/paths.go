package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DefaultMaxPaths caps how many paths are listed per workflow.
const DefaultMaxPaths = 2000

// Step is one junction passed on a path, and the exit taken.
type Step struct {
	Junction *Junction
	Exit     *Exit
}

// Path is one complete trip from Start to an end station.
type Path struct {
	Steps []Step
	End   EndKind
}

// Key is the path's identity: its exit IDs and its end kind, e.g.
// "J1.false J2.success|completed".
func (p Path) Key() string {
	ids := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ids[i] = s.Exit.ID.String()
	}
	return strings.Join(ids, " ") + "|" + string(p.End)
}

// ID is a short, stable hash of Key, so the same trip has the same ID in
// analyze, coverage and report.
func (p Path) ID() string {
	sum := sha256.Sum256([]byte(p.Key()))
	return hex.EncodeToString(sum[:])[:10]
}

// PathList is the listed paths of one workflow.
type PathList struct {
	List      []Path
	Truncated bool // more paths exist than max
}

// Paths lists every path, depth first, taking exits in display order.
// Each exit, and each loop's retry edge, is used at most once per path
// (the analyzer half of the loop rule, CLAUDE.md D3): so a loop adds the
// paths "not entered" (exit), "entered and left from inside the body"
// (iterate, then return or break) and "entered, went round, then left"
// (iterate, body, retry, exit). A road that would need a second retry is
// not listed. At most max paths are listed.
func (g *Graph) Paths(max int) PathList {
	var out PathList
	used := map[EdgeID]bool{}
	var steps []Step
	var walk func(t Target)
	walk = func(t Target) {
		if out.Truncated || t.Dead() {
			return
		}
		if r := t.Retry; r != nil {
			if used[r.ID] {
				return
			}
			used[r.ID] = true
			steps = append(steps, Step{Junction: r.Junction, Exit: r})
			walk(r.To)
			steps = steps[:len(steps)-1]
			used[r.ID] = false
			return
		}
		if t.Junction == nil {
			if len(out.List) == max {
				out.Truncated = true
				return
			}
			out.List = append(out.List, Path{Steps: append([]Step(nil), steps...), End: t.End})
			return
		}
		for _, e := range t.Junction.Exits {
			if used[e.ID] {
				continue
			}
			used[e.ID] = true
			steps = append(steps, Step{Junction: t.Junction, Exit: e})
			walk(e.To)
			steps = steps[:len(steps)-1]
			used[e.ID] = false
		}
	}
	walk(g.Start)
	return out
}
