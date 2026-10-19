package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// DefaultStageMaxPaths is how many paths a stage may have before it is not
// merged with its neighbour (classification.stages.max_paths).
const DefaultStageMaxPaths = 256

// A Stage is a stretch of a workflow's map between two gates. A gate is a
// junction that every path getting past it has to go through: no road from
// before it reaches anything after it except through it. The paths of the
// workflow are then the stage paths of stage 1, followed (for those that do
// not end early) by those of stage 2, and so on, so the stages can be
// counted, classified and covered one at a time, however many paths the
// whole workflow has.
type Stage struct {
	// Index counts from 0; the reports say "stage 1" for Index 0.
	Index int
	// Entry is where the stage starts: the workflow's start for the first
	// stage, a gate for the others.
	Entry Target
	// Next is the gate that ends the stage (the first junction of the next
	// stage); nil for the last stage.
	Next *Junction
	// Junctions are the decisions inside the stage.
	Junctions []*Junction
	// Count is how many stage paths there are: the paths that end in the stage
	// plus the ones that go on to the next stage.
	Count *big.Int
	// Big: Count is more than the limit, even for this stage alone. It is not
	// listed, and the reports use branch coverage inside it.
	Big bool

	// Part says the stage is a piece of a stretch that was too big for one
	// stage, cut at the decisions that every path through a nested region of
	// it passes (see splitBig). A part is examined by the fact engine with
	// nothing known on the way in, and leaves nothing known: what it claims
	// rests on its own decisions only. Within is its set of decisions; a stage
	// path ends when it reaches a decision outside it. SplitFrom is how many
	// paths the stretch had.
	Part      bool
	Within    map[*Junction]bool
	SplitFrom *big.Int
	// SplitWhy, on a Big stage that could not be cut, says why in a line.
	SplitWhy string
}

// Stops reports whether a path of the stage ends when it reaches junction j
// (the stage's next gate, or for a part any decision outside it).
func (st Stage) Stops(j *Junction) bool {
	if st.Part {
		return !st.Within[j]
	}
	return st.Next != nil && j == st.Next
}

// Stages cuts the workflow into stages. Stages next to each other are merged
// while the merged stage has at most maxPaths stage paths.
func (g *Graph) Stages(maxPaths int) []Stage {
	if g.stagesFor == maxPaths && g.stages != nil {
		return g.stages
	}
	g.stages, g.stagesFor = g.computeStages(maxPaths), maxPaths
	return g.stages
}

// StageOf is the index of the stage a junction is in, for the stages of the
// last Stages call.
func (g *Graph) StageOf(j *Junction) int {
	if g.stageOf == nil {
		return 0
	}
	return g.stageOf[j]
}

func (g *Graph) computeStages(maxPaths int) []Stage {
	g.stageOf = map[*Junction]int{}
	if g.Start.Junction == nil || len(g.Junctions) == 0 {
		return []Stage{{Index: 0, Entry: g.Start, Count: g.PathCount()}}
	}
	// The graph of decisions: an edge for every exit that leads to another
	// decision. A loop's retry edge goes back and is left out, which makes it a
	// DAG.
	idx := map[*Junction]int{}
	var nodes []*Junction
	var succ [][]int
	add := func(j *Junction) int {
		if i, ok := idx[j]; ok {
			return i
		}
		i := len(nodes)
		idx[j] = i
		nodes = append(nodes, j)
		succ = append(succ, nil)
		return i
	}
	// Depth first from the start, so every node is reached; the post order
	// gives a topological order.
	var order []int
	var back [][2]int            // retry edges: from a decision in a loop's body to the loop's head
	restricted := map[int]bool{} // decisions some road reaches with only one exit allowed
	state := map[int]int{}       // 1 = on the stack, 2 = done
	var visit func(i int)
	visit = func(i int) {
		state[i] = 1
		seen := map[int]bool{}
		for _, e := range nodes[i].Exits {
			if e.To.Retry != nil {
				back = append(back, [2]int{i, add(e.To.Retry.Junction)})
				continue
			}
			if e.To.Junction == nil {
				continue
			}
			k := add(e.To.Junction)
			if e.To.Only != nil {
				restricted[k] = true // what follows depends on the road in: not a gate
			}
			if !seen[k] {
				seen[k] = true
				succ[i] = append(succ[i], k)
			}
			if state[k] == 0 {
				visit(k)
			}
		}
		state[i] = 2
		order = append(order, i)
	}
	root := add(g.Start.Junction)
	visit(root)
	n := len(nodes)
	// reverse post order
	rpo := make([]int, n)
	pos := make([]int, n)
	for i := range order {
		rpo[n-1-i] = order[i]
	}
	for p, v := range rpo {
		pos[v] = p
	}
	// Dominators (Cooper, Harvey, Kennedy) on the DAG.
	idom := make([]int, n)
	for i := range idom {
		idom[i] = -1
	}
	idom[root] = root
	preds := make([][]int, n)
	for u := range succ {
		for _, v := range succ[u] {
			preds[v] = append(preds[v], u)
		}
	}
	intersect := func(a, b int) int {
		for a != b {
			for pos[a] > pos[b] {
				a = idom[a]
			}
			for pos[b] > pos[a] {
				b = idom[b]
			}
		}
		return a
	}
	for _, v := range rpo {
		if v == root {
			continue
		}
		d := -1
		for _, p := range preds[v] {
			if idom[p] == -1 {
				continue
			}
			if d == -1 {
				d = p
			} else {
				d = intersect(p, d)
			}
		}
		idom[v] = d
	}
	// Subtree sizes of the dominator tree, and entry/exit numbers.
	kids := make([][]int, n)
	for v := 0; v < n; v++ {
		if v != root && idom[v] >= 0 {
			kids[idom[v]] = append(kids[idom[v]], v)
		}
	}
	tin := make([]int, n)
	tout := make([]int, n)
	size := make([]int, n)
	clock := 0
	var walk func(v int)
	walk = func(v int) {
		tin[v] = clock
		clock++
		size[v] = 1
		for _, k := range kids[v] {
			walk(k)
			size[v] += size[k]
		}
		tout[v] = clock
	}
	walk(root)
	// What each node can reach.
	words := (n + 63) / 64
	reach := make([][]uint64, n)
	for i := len(rpo) - 1; i >= 0; i-- {
		v := rpo[i]
		bits := make([]uint64, words)
		for _, s := range succ[v] {
			bits[s/64] |= 1 << (s % 64)
			for w, x := range reach[s] {
				bits[w] |= x
			}
		}
		reach[v] = bits
	}
	reachCount := func(v int) int {
		c := 0
		for _, x := range reach[v] {
			for ; x != 0; x &= x - 1 {
				c++
			}
		}
		return c
	}
	// A gate dominates everything it can reach, and does not cut a loop: a
	// retry edge from inside the gate's part of the map goes back to a loop head
	// that is inside it too.
	var gates []int
	for v := 0; v < n; v++ {
		if reachCount(v) != size[v]-1 || restricted[v] {
			continue
		}
		cuts := false
		for _, b := range back {
			inside := func(x int) bool { return tin[v] <= tin[x] && tin[x] < tout[v] }
			if inside(b[0]) && !inside(b[1]) {
				cuts = true
				break
			}
		}
		if !cuts {
			gates = append(gates, v)
		}
	}
	sort.Slice(gates, func(a, b int) bool { return tin[gates[a]] < tin[gates[b]] })

	// Minimal stages: the nodes of gate k's subtree not in gate k+1's.
	type minimal struct {
		entry Target
		gate  int // -1 for the stretch before the first gate
		next  int // the gate that ends it, -1 for the last
	}
	var mins []minimal
	if len(gates) == 0 || gates[0] != root {
		next := -1
		if len(gates) > 0 {
			next = gates[0]
		}
		mins = append(mins, minimal{entry: g.Start, gate: -1, next: next})
	}
	for i, v := range gates {
		next := -1
		if i+1 < len(gates) {
			next = gates[i+1]
		}
		mins = append(mins, minimal{entry: Target{Junction: nodes[v]}, gate: v, next: next})
	}
	// Merge neighbours while the merged stage stays small.
	var stages []Stage
	for a := 0; a < len(mins); {
		b := a
		cnt := newCounter(g, stopOf(nodes, mins[b].next)).count(mins[a].entry)
		for b+1 < len(mins) {
			merged := newCounter(g, stopOf(nodes, mins[b+1].next)).count(mins[a].entry)
			if merged.Cmp(big.NewInt(int64(maxPaths))) > 0 {
				break
			}
			cnt = merged
			b++
		}
		st := Stage{Index: len(stages), Entry: mins[a].entry, Next: stopOf(nodes, mins[b].next), Count: cnt}
		st.Big = cnt.Cmp(big.NewInt(int64(maxPaths))) > 0
		// the nodes: in the subtree of the first gate (or before the first gate)
		// and not in the subtree of the stage's end gate
		for v := 0; v < n; v++ {
			in := false
			if mins[a].gate == -1 {
				in = mins[b].next == -1 || !(tin[mins[b].next] <= tin[v] && tin[v] < tout[mins[b].next])
			} else {
				in = tin[mins[a].gate] <= tin[v] && tin[v] < tout[mins[a].gate] &&
					(mins[b].next == -1 || !(tin[mins[b].next] <= tin[v] && tin[v] < tout[mins[b].next]))
			}
			if in {
				st.Junctions = append(st.Junctions, nodes[v])
				g.stageOf[nodes[v]] = st.Index
			}
		}
		sort.Slice(st.Junctions, func(x, y int) bool { return lessOrder(st.Junctions[x].order, st.Junctions[y].order) })
		stages = append(stages, st)
		a = b + 1
	}
	return g.splitBigStages(stages, maxPaths)
}

func stopOf(nodes []*Junction, i int) *Junction {
	if i < 0 {
		return nil
	}
	return nodes[i]
}

// A StagePath is one way through a stage: its steps, then either how the
// workflow ends or that it goes on to the next stage.
type StagePath struct {
	Stage    int
	Steps    []Step
	End      EndKind
	Continue bool // goes on to the next stage (End is then unused)
}

// Key is the stage path's identity in exit IDs, like Path.Key.
func (p StagePath) Key() string {
	ids := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ids[i] = s.Exit.ID.String()
	}
	return strings.Join(ids, " ") + "|" + p.endText()
}

func (p StagePath) endText() string {
	if p.Continue {
		return ">"
	}
	return string(p.End)
}

// StableKey is the stage path in words, like Path.StableKey.
func (p StagePath) StableKey() string {
	parts := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		parts[i] = s.Junction.stable + "\x1f" + strings.Join(strings.Fields(s.Exit.Label), " ")
	}
	return strings.Join(parts, "\n") + "|" + p.endText()
}

// StageIDVersion prefixes the hash of every stage path ID.
const StageIDVersion = "g1"

// StageID is a short hash of the stage path's words with the stage number in
// front: "g2:3fa94c0d17ab". Like StableID, it stays the same when unrelated
// code around it changes.
func (p StagePath) StageID() string {
	sum := sha256.Sum256([]byte(StageIDVersion + "\n" + fmt.Sprint(p.Stage) + "\n" + p.StableKey()))
	return fmt.Sprintf("g%d:%s", p.Stage+1, hex.EncodeToString(sum[:])[:12])
}

// StagePaths lists the paths of a stage, at most limit of them (and so
// Truncated tells whether there are more).
func (g *Graph) StagePaths(st Stage, limit int) (out []StagePath, truncated bool) {
	return g.stagePaths(st, limit, map[EdgeID]bool{})
}

// StagePathsFrom lists the stage paths that start at st.Entry when the exits
// in used are already taken (the walk is part way along a stage).
func (g *Graph) StagePathsFrom(st Stage, used map[EdgeID]bool) ([]StagePath, bool) {
	cp := make(map[EdgeID]bool, len(used))
	for k, v := range used {
		cp[k] = v
	}
	return g.stagePaths(st, 1<<30, cp)
}

func (g *Graph) stagePaths(st Stage, limit int, used map[EdgeID]bool) (out []StagePath, truncated bool) {
	var steps []Step
	var walk func(t Target)
	walk = func(t Target) {
		if truncated || t.Dead() {
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
		if t.Junction == nil || st.Stops(t.Junction) {
			if len(out) == limit {
				truncated = true
				return
			}
			p := StagePath{Stage: st.Index, Steps: append([]Step(nil), steps...)}
			if t.Junction != nil {
				p.Continue = true
			} else {
				p.End = t.End
			}
			out = append(out, p)
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
	walk(st.Entry)
	return out, truncated
}

// StagePieces cuts a path into its stage pieces, one per stage the path
// reaches, in order. A path that ends in a stage has that stage's piece end
// with the end of the workflow; the others go on.
func (g *Graph) StagePieces(p Path, stages []Stage) []StagePath {
	if len(stages) == 0 {
		return nil
	}
	// The stage of a junction comes from the stages passed in. The cached map
	// is only valid for the last Stages call, so another set builds its own.
	of := g.stageOf
	if len(g.stages) == 0 || &stages[0] != &g.stages[0] {
		of = map[*Junction]int{}
		for _, st := range stages {
			for _, j := range st.Junctions {
				of[j] = st.Index
			}
		}
	}
	var out []StagePath
	closeCur := func(cur StagePath) {
		// A part of a cut stretch that the path skips (the nested region was not
		// entered) has no piece.
		if len(cur.Steps) == 0 && cur.Stage < len(stages) && stages[cur.Stage].Part {
			return
		}
		out = append(out, cur)
	}
	cur := StagePath{Stage: 0}
	for _, s := range p.Steps {
		k := of[s.Junction]
		for cur.Stage < k {
			cur.Continue = true
			closeCur(cur)
			cur = StagePath{Stage: cur.Stage + 1}
		}
		cur.Steps = append(cur.Steps, s)
	}
	cur.End = p.End
	out = append(out, cur)
	return out
}
