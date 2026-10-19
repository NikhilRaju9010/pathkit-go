package model

import (
	"math/big"
	"sort"
)

// A stage that has more paths than classification.stages.max_paths, even alone,
// is cut further instead of falling back to branch coverage. Inside such a
// stretch no decision is passed by every path, but there are nested regions: a
// decision whose one side opens a run of decisions that rejoin the other side
// only at the end (an "if" around twenty others). Inside that region the first
// decision is passed by every path that enters it, and so is each decision that
// follows it in the chain: the region has gates of its own. splitBig cuts at
// those, recursively, until the pieces are small or cannot be cut, and says why
// when they cannot.

const maxSplitDepth = 4

// splitBigStages replaces every Big stage that can be cut by its parts, and
// numbers the stages again from 0.
func (g *Graph) splitBigStages(stages []Stage, maxPaths int) []Stage {
	var out []Stage
	for _, st := range stages {
		if !st.Big {
			out = append(out, st)
			continue
		}
		parts, why := g.splitBig(st, maxPaths, 0)
		if len(parts) == 0 {
			st.SplitWhy = why
			out = append(out, st)
			continue
		}
		out = append(out, parts...)
	}
	g.stageOf = map[*Junction]int{}
	for i := range out {
		out[i].Index = i
		for _, j := range out[i].Junctions {
			g.stageOf[j] = i
		}
	}
	return out
}

// splitBig cuts st (a stage, or a part, with more than maxPaths stage paths)
// into parts, in the order every path goes through them. It returns nil and the
// reason when it cannot.
func (g *Graph) splitBig(st Stage, maxPaths, depth int) ([]Stage, string) {
	if depth >= maxSplitDepth {
		return nil, "it is nested too deep to cut further"
	}
	if st.Entry.Junction == nil || len(st.Junctions) < 2 {
		return nil, "it is a single decision"
	}
	in := map[*Junction]bool{}
	for _, j := range st.Junctions {
		in[j] = true
	}
	root := st.Entry.Junction
	if !in[root] {
		return nil, "its first decision is not part of it"
	}
	d, why := newDomInfo(in, root)
	if d == nil {
		return nil, why
	}
	n, nodes, succ, pos, idom := d.n, d.nodes, d.succ, d.pos, d.idom
	r, restricted, back := d.root, d.restricted, d.back
	inSub := d.inSub
	// Local gates: a decision other than the first that dominates everything it
	// reaches inside the stretch, takes no single road in, and cuts no loop.
	isGate := make([]bool, n)
	for v := 0; v < n; v++ {
		if v == r || d.reachCount(v) != d.size[v]-1 || restricted[v] {
			continue
		}
		cuts := false
		for _, b := range back {
			if inSub(v, b[0]) && !inSub(v, b[1]) {
				cuts = true
				break
			}
		}
		isGate[v] = !cuts
	}
	// The outermost gates open the regions.
	var openers []int
	for v := 0; v < n; v++ {
		if !isGate[v] {
			continue
		}
		nested := false
		for a := idom[v]; a != r && a >= 0; a = idom[a] {
			if isGate[a] {
				nested = true
				break
			}
		}
		if !nested && idom[v] >= 0 {
			openers = append(openers, v)
		}
	}
	if len(openers) == 0 {
		return nil, "no decision inside it is passed by every path that enters the part of it below that decision"
	}
	sort.Slice(openers, func(a, b int) bool { return pos[openers[a]] < pos[openers[b]] })
	inRegion := make([]int, n) // which opener's region a node is in, -1 for the top
	for i := range inRegion {
		inRegion[i] = -1
	}
	for k, o := range openers {
		for x := 0; x < n; x++ {
			if inSub(o, x) {
				inRegion[x] = k
			}
		}
	}
	// The top part must come before every region on every path: no road leads
	// from a region back to a decision of the top part.
	for u := range succ {
		if inRegion[u] < 0 {
			continue
		}
		for _, v := range succ[u] {
			if inRegion[v] < 0 {
				return nil, "decisions of the top of it are reached again from inside a nested region"
			}
		}
	}
	// Regions must come in an order every path agrees with: a road from one
	// region into another goes forwards.
	for u := range succ {
		for _, v := range succ[u] {
			if inRegion[u] >= 0 && inRegion[v] >= 0 && inRegion[u] != inRegion[v] && inRegion[u] > inRegion[v] {
				return nil, "its nested regions lead into each other in no fixed order"
			}
		}
	}
	// Build the parts: the top, then each region, cut at its own gates.
	set := func(pred func(int) bool) (map[*Junction]bool, []*Junction) {
		m := map[*Junction]bool{}
		var js []*Junction
		for v := 0; v < n; v++ {
			if pred(v) {
				m[nodes[v]] = true
				js = append(js, nodes[v])
			}
		}
		sort.Slice(js, func(a, b int) bool { return lessOrder(js[a].order, js[b].order) })
		return m, js
	}
	count := func(entry Target, within map[*Junction]bool) *big.Int {
		c := newCounter(g, nil)
		c.outside = within
		return c.count(entry)
	}
	var parts []Stage
	mk := func(entry Target, within map[*Junction]bool, js []*Junction) Stage {
		cnt := count(entry, within)
		return Stage{Entry: entry, Count: cnt, Big: cnt.Cmp(big.NewInt(int64(maxPaths))) > 0, Junctions: js, Part: true, Within: within, SplitFrom: st.Count}
	}
	topSet, topJs := set(func(v int) bool { return inRegion[v] < 0 })
	parts = append(parts, mk(st.Entry, topSet, topJs))
	for k, o := range openers {
		regionSet, regionJs := set(func(v int) bool { return inRegion[v] == k })
		parts = append(parts, g.cutRegion(nodes[o], regionSet, regionJs, st, maxPaths, depth)...)
	}
	// Parts still too big are cut again.
	var final []Stage
	for _, p := range parts {
		if !p.Big {
			final = append(final, p)
			continue
		}
		sub, why := g.splitBig(p, maxPaths, depth+1)
		if len(sub) == 0 {
			p.SplitWhy = why
			final = append(final, p)
			continue
		}
		final = append(final, sub...)
	}
	return final, ""
}

// cutRegion cuts a region (the decisions below one opening decision) into
// parts at the decisions that every path through it passes, merging
// neighbours while they stay within maxPaths stage paths.
func (g *Graph) cutRegion(open *Junction, within map[*Junction]bool, js []*Junction, whole Stage, maxPaths, depth int) []Stage {
	region := Stage{Entry: Target{Junction: open}, Junctions: js, Part: true, Within: within, SplitFrom: whole.Count}
	c := newCounter(g, nil)
	c.outside = within
	region.Count = c.count(region.Entry)
	region.Big = region.Count.Cmp(big.NewInt(int64(maxPaths))) > 0
	if !region.Big {
		return []Stage{region}
	}
	// One more cut: the region, taken as a stretch of its own, has gates of its
	// own, the first of which is its opener.
	sub, _ := g.cutAtGates(region, maxPaths)
	if len(sub) == 0 {
		return []Stage{region}
	}
	return sub
}

// cutAtGates cuts a part that is a chain: every decision after the first that
// dominates everything it reaches in the part is a gate, and the stretches
// between neighbouring gates are the pieces, merged while they stay small.
func (g *Graph) cutAtGates(st Stage, maxPaths int) ([]Stage, string) {
	d, why := newDomInfo(st.Within, st.Entry.Junction)
	if d == nil {
		return nil, why
	}
	n, nodes, restricted, back := d.n, d.nodes, d.restricted, d.back
	tin, tout, r := d.tin, d.tout, d.root
	var gates []int
	for v := 0; v < n; v++ {
		if d.reachCount(v) != d.size[v]-1 || restricted[v] {
			continue
		}
		cuts := false
		for _, b := range back {
			if tin[v] <= tin[b[0]] && tin[b[0]] < tout[v] && !(tin[v] <= tin[b[1]] && tin[b[1]] < tout[v]) {
				cuts = true
				break
			}
		}
		if !cuts {
			gates = append(gates, v)
		}
	}
	sort.Slice(gates, func(a, b int) bool { return tin[gates[a]] < tin[gates[b]] })
	if len(gates) < 2 || gates[0] != r {
		return nil, "no decision after the first is passed by every path"
	}
	// Minimal pieces: from gate k up to (not including) gate k+1.
	type piece struct {
		entry Target
		js    []*Junction
		set   map[*Junction]bool
	}
	var pieces []piece
	for i, v := range gates {
		p := piece{entry: Target{Junction: nodes[v]}, set: map[*Junction]bool{}}
		for x := 0; x < n; x++ {
			if tin[v] <= tin[x] && tin[x] < tout[v] && (i+1 >= len(gates) || !(tin[gates[i+1]] <= tin[x] && tin[x] < tout[gates[i+1]])) {
				p.js = append(p.js, nodes[x])
				p.set[nodes[x]] = true
			}
		}
		pieces = append(pieces, p)
	}
	limit := big.NewInt(int64(maxPaths))
	var out []Stage
	for a := 0; a < len(pieces); {
		merged := map[*Junction]bool{}
		var js []*Junction
		b := a
		var cnt *big.Int
		for ; b < len(pieces); b++ {
			trialSet := map[*Junction]bool{}
			for k := range merged {
				trialSet[k] = true
			}
			for k := range pieces[b].set {
				trialSet[k] = true
			}
			c := newCounter(g, nil)
			c.outside = trialSet
			next := c.count(pieces[a].entry)
			if b > a && next.Cmp(limit) > 0 {
				break
			}
			merged, cnt = trialSet, next
			js = append(js, pieces[b].js...)
		}
		sort.Slice(js, func(x, y int) bool { return lessOrder(js[x].order, js[y].order) })
		out = append(out, Stage{Entry: pieces[a].entry, Count: cnt, Big: cnt.Cmp(limit) > 0, Junctions: js, Part: true, Within: merged, SplitFrom: st.SplitFrom})
		a = b
	}
	return out, ""
}

// domInfo is the graph of the decisions of a stretch (the exits that lead to
// another decision of it, without the retry edges that go back to a loop head),
// with the dominator tree computed on it.
type domInfo struct {
	n          int
	nodes      []*Junction
	succ       [][]int
	root       int
	pos        []int // position in reverse post order
	idom       []int
	tin, tout  []int // dominator tree entry and exit numbers
	size       []int // size of each node's subtree in the dominator tree
	reach      [][]uint64
	restricted map[int]bool // decisions some road reaches with only one exit allowed
	back       [][2]int     // retry edges: from a decision in a loop's body to the loop's head
}

func (d *domInfo) inSub(v, x int) bool { return d.tin[v] <= d.tin[x] && d.tin[x] < d.tout[v] }

func (d *domInfo) reachCount(v int) int {
	c := 0
	for _, x := range d.reach[v] {
		for ; x != 0; x &= x - 1 {
			c++
		}
	}
	return c
}

// newDomInfo builds the decision graph of the set in, from root, with
// dominators (Cooper, Harvey, Kennedy). It returns nil and the reason when a
// decision of the set cannot be reached from root.
func newDomInfo(in map[*Junction]bool, root *Junction) (*domInfo, string) {
	d := &domInfo{restricted: map[int]bool{}}
	idx := map[*Junction]int{}
	add := func(j *Junction) int {
		if i, ok := idx[j]; ok {
			return i
		}
		idx[j] = len(d.nodes)
		d.nodes = append(d.nodes, j)
		d.succ = append(d.succ, nil)
		return idx[j]
	}
	var order []int
	state := map[int]int{}
	var visit func(i int)
	visit = func(i int) {
		state[i] = 1
		seen := map[int]bool{}
		for _, e := range d.nodes[i].Exits {
			if e.To.Retry != nil {
				if in[e.To.Retry.Junction] {
					d.back = append(d.back, [2]int{i, add(e.To.Retry.Junction)})
				}
				continue
			}
			if e.To.Junction == nil || !in[e.To.Junction] {
				continue
			}
			k := add(e.To.Junction)
			if e.To.Only != nil {
				d.restricted[k] = true
			}
			if !seen[k] {
				seen[k] = true
				d.succ[i] = append(d.succ[i], k)
			}
			if state[k] == 0 {
				visit(k)
			}
		}
		state[i] = 2
		order = append(order, i)
	}
	d.root = add(root)
	visit(d.root)
	n := len(d.nodes)
	d.n = n
	if n != len(in) {
		return nil, "part of it cannot be reached from its first decision"
	}
	rpo := make([]int, n)
	d.pos = make([]int, n)
	for i := range order {
		rpo[n-1-i] = order[i]
	}
	for p, v := range rpo {
		d.pos[v] = p
	}
	preds := make([][]int, n)
	for u := range d.succ {
		for _, v := range d.succ[u] {
			preds[v] = append(preds[v], u)
		}
	}
	d.idom = make([]int, n)
	for i := range d.idom {
		d.idom[i] = -1
	}
	d.idom[d.root] = d.root
	intersect := func(a, b int) int {
		for a != b {
			for d.pos[a] > d.pos[b] {
				a = d.idom[a]
			}
			for d.pos[b] > d.pos[a] {
				b = d.idom[b]
			}
		}
		return a
	}
	for _, v := range rpo {
		if v == d.root {
			continue
		}
		dd := -1
		for _, p := range preds[v] {
			if d.idom[p] == -1 {
				continue
			}
			if dd == -1 {
				dd = p
			} else {
				dd = intersect(p, dd)
			}
		}
		d.idom[v] = dd
	}
	kids := make([][]int, n)
	for v := 0; v < n; v++ {
		if v != d.root && d.idom[v] >= 0 {
			kids[d.idom[v]] = append(kids[d.idom[v]], v)
		}
	}
	d.tin, d.tout, d.size = make([]int, n), make([]int, n), make([]int, n)
	clock := 0
	var walk func(v int)
	walk = func(v int) {
		d.tin[v] = clock
		clock++
		d.size[v] = 1
		for _, k := range kids[v] {
			walk(k)
			d.size[v] += d.size[k]
		}
		d.tout[v] = clock
	}
	walk(d.root)
	words := (n + 63) / 64
	d.reach = make([][]uint64, n)
	for i := n - 1; i >= 0; i-- {
		v := rpo[i]
		bits := make([]uint64, words)
		for _, s := range d.succ[v] {
			bits[s/64] |= 1 << (s % 64)
			for w, x := range d.reach[s] {
				bits[w] |= x
			}
		}
		d.reach[v] = bits
	}
	return d, ""
}
