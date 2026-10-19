package model

// Loop rounds. PathKit's path model stays one round per loop (docs/DESIGN.md
// D3): a path goes into a loop body once. A recorded run does not: it records
// every trip, so from the steps of a run one can tell how many rounds each loop
// ran (0, 1 or more). The "loop rounds" table shows which of the three some test
// saw, and which can happen at all.

// RoundBuckets are the three counts a loop is judged by.
var RoundBuckets = []string{"0 rounds", "1 round", "2+ rounds"}

// Loops lists the loops of the map (the workflow's, those in copies of helper
// code, and those in summarized helpers), in order of position.
func (g *Graph) Loops() []*Junction {
	var out []*Junction
	for _, j := range g.AllJunctions() {
		if j.Kind == Loop {
			out = append(out, j)
		}
	}
	return out
}

// CanRun0 reports whether the loop can run no round at all. A loop with no exit
// of its own ("for {}") and one whose condition is true on the first test (the
// extractor found it) always runs at least once.
func (j *Junction) CanRun0() bool {
	hasExit := false
	for _, e := range j.Exits {
		if e.Label == "exit" {
			hasExit = true
		}
	}
	if !hasExit {
		return false
	}
	for _, f := range j.Facts {
		if f.Kind == LoopFirstTripAlways {
			return false
		}
	}
	return true
}

// Rounds reads one recorded run: for every loop it went through, how many
// rounds each entry into the loop ran. A loop entered again (because an outer
// loop went round) is a new entry.
func (g *Graph) Rounds(steps []string) map[*Junction][]int {
	loops := g.Loops()
	byEdge := map[string]*Junction{}
	kind := map[string]string{}
	for _, j := range loops {
		exits := j.Exits
		if j.Retry != nil {
			exits = append(exits[:len(exits):len(exits)], j.Retry)
		}
		for _, e := range exits {
			byEdge[e.ID.String()] = j
			kind[e.ID.String()] = e.Label
		}
	}
	contains := func(outer, inner *Junction) bool {
		o, i := outer.Stmt, inner.Stmt
		return outer != inner && o.Pos() <= i.Pos() && i.End() <= o.End()
	}
	open := map[*Junction]int{} // rounds so far in the current entry (absent: not in one)
	out := map[*Junction][]int{}
	for _, s := range steps {
		j := byEdge[s]
		if j == nil {
			continue
		}
		// a trip of an outer loop starts the loops inside it afresh
		if k := kind[s]; k == "iterate" || k == "retry" {
			for _, in := range loops {
				if contains(j, in) {
					if n, ok := open[in]; ok {
						out[in] = append(out[in], n)
						delete(open, in)
					}
				}
			}
		}
		switch kind[s] {
		case "iterate":
			open[j]++
		case "exit":
			out[j] = append(out[j], open[j])
			delete(open, j)
		}
	}
	for j, n := range open { // left by a break, or the run ended inside the loop
		out[j] = append(out[j], n)
	}
	return out
}

// RoundBucket names the bucket a round count falls in.
func RoundBucket(n int) string {
	switch {
	case n <= 0:
		return RoundBuckets[0]
	case n == 1:
		return RoundBuckets[1]
	}
	return RoundBuckets[2]
}
