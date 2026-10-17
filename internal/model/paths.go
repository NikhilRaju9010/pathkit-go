package model

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
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
	// Compensation: the path registers a saga compensation defer, shown
	// as the note "[compensation (defer)]". It is not part of Key: the
	// steps decide it (see compensationOn).
	Compensation bool
}

// CompensationNote is how the note is printed after the end station.
const CompensationNote = "[compensation (defer)]"

// compensationOn reports whether a path with these steps passes a saga
// compensation defer: on the road from Start, or on the road after any
// step. Paths and Match both use it, so the listing and the matcher agree.
func (g *Graph) compensationOn(steps []Step) bool {
	if g.Start.Compensation {
		return true
	}
	for _, s := range steps {
		if s.Exit.To.Compensation {
			return true
		}
	}
	return false
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
// (the analyzer half of the loop rule, docs/DESIGN.md D3): so a loop adds the
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
			p := Path{Steps: append([]Step(nil), steps...), End: t.End}
			p.Compensation = g.compensationOn(p.Steps)
			out.List = append(out.List, p)
			return
		}
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*Exit{t.Only}
		}
		for _, e := range exits {
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

// A PrunedGroup is a part of the map that listing leaves out because every
// path through it is shown impossible: the steps that lead to the impossible
// exit (the exit is the last one), how many paths are below it, and whatever
// the caller recorded as the reason.
type PrunedGroup struct {
	Prefix []Step
	Count  *big.Int
	Info   any
}

// PrunedList is Paths with the impossible parts left out and counted.
type PrunedList struct {
	PathList
	Pruned []PrunedGroup
}

// PathsPruned lists paths like Paths, but follows a state along each path and
// leaves out every exit the state shows impossible. root makes the state at
// the start, clone copies a state, and step takes one decision on a state and
// returns nil, or the reason the decision cannot be taken (then nothing below
// that exit is listed, only counted). The listed paths and their IDs are
// exactly the ones Paths would list from the same place, minus the pruned
// ones, so the cap fills with paths that can be taken.
func (g *Graph) PathsPruned(max int, root func() any, clone func(any) any, step func(state any, s Step) any) PrunedList {
	var out PrunedList
	c := newCounter(g, nil)
	used := map[EdgeID]bool{}
	var steps []Step
	var walk func(t Target, state any)
	take := func(s Step, state any, last bool) (any, bool) {
		used[s.Exit.ID] = true
		steps = append(steps, s)
		branch := state
		if !last {
			branch = clone(state)
		}
		if info := step(branch, s); info != nil {
			out.Pruned = append(out.Pruned, PrunedGroup{Prefix: append([]Step(nil), steps...), Count: new(big.Int).Set(c.count(s.Exit.To)), Info: info})
			steps = steps[:len(steps)-1]
			used[s.Exit.ID] = false
			return nil, false
		}
		return branch, true
	}
	walk = func(t Target, state any) {
		if out.Truncated || t.Dead() {
			return
		}
		if r := t.Retry; r != nil {
			if used[r.ID] {
				return
			}
			if branch, ok := take(Step{Junction: r.Junction, Exit: r}, state, true); ok {
				walk(r.To, branch)
				steps = steps[:len(steps)-1]
				used[r.ID] = false
			}
			return
		}
		if t.Junction == nil {
			if len(out.List) == max {
				out.Truncated = true
				return
			}
			p := Path{Steps: append([]Step(nil), steps...), End: t.End}
			p.Compensation = g.compensationOn(p.Steps)
			out.List = append(out.List, p)
			return
		}
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*Exit{t.Only}
		}
		var avail []*Exit
		for _, e := range exits {
			if !used[e.ID] {
				avail = append(avail, e)
			}
		}
		for i, e := range avail {
			if branch, ok := take(Step{Junction: t.Junction, Exit: e}, state, i == len(avail)-1); ok {
				walk(e.To, branch)
				steps = steps[:len(steps)-1]
				used[e.ID] = false
			}
		}
	}
	walk(g.Start, root())
	return out
}

// ReachableExits lists the exits that appear on at least one path, in
// junction order, and the same for loops' retry edges. (An exit the map draws
// but no path can take, because the road in already decided the decision, is
// not one of them.)
func (g *Graph) ReachableExits() []*Exit {
	seenT := map[countKey]bool{}
	seenE := map[*Exit]bool{}
	var walk func(t Target)
	walk = func(t Target) {
		if t.Dead() {
			return
		}
		k := keyOf(t)
		if seenT[k] {
			return
		}
		seenT[k] = true
		if r := t.Retry; r != nil {
			seenE[r] = true
			for _, e := range r.Junction.Exits {
				if e.Label == "exit" {
					walk(e.To)
				}
			}
			return
		}
		if t.Junction == nil {
			return
		}
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*Exit{t.Only}
		}
		for _, e := range exits {
			seenE[e] = true
			walk(e.To)
		}
	}
	walk(g.Start)
	var out []*Exit
	for _, j := range g.Junctions {
		for _, e := range j.Exits {
			if seenE[e] {
				out = append(out, e)
			}
		}
		if j.Retry != nil && seenE[j.Retry] {
			out = append(out, j.Retry)
		}
	}
	return out
}
