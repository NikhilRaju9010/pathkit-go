package model

// A DecisionPair is the outcome of one decision followed directly by the
// outcome of the next decision on a road (docs/DESIGN.md D27): "J1.true and
// then J2.failure". Pairs are a finer level than branches (each exit) and a
// coarser one than paths; they are never a main number.
type DecisionPair struct{ First, Second *Exit }

// Key names the pair for matching: "J1.true J2.failure".
func (p DecisionPair) Key() string { return p.First.ID.String() + " " + p.Second.ID.String() }

// DecisionPairs lists every pair of exits where the second exit belongs to the
// next decision after the first on some road of the map. A loop's retry edge is
// not a decision: after the body, the next decision is the loop's own exit.
// Pairs the engine shows impossible are not removed here (the map does not
// know); coverage removes them.
func (g *Graph) DecisionPairs() []DecisionPair {
	var out []DecisionPair
	seen := map[string]bool{}
	add := func(a, b *Exit) {
		p := DecisionPair{a, b}
		if !seen[p.Key()] {
			seen[p.Key()] = true
			out = append(out, p)
		}
	}
	// next gives the exits a road can take at its first decision.
	next := func(t Target) []*Exit {
		switch {
		case t.Dead():
			return nil
		case t.Retry != nil:
			for _, e := range t.Retry.Junction.Exits {
				if e.Label == "exit" {
					return []*Exit{e}
				}
			}
			return nil
		case t.Junction != nil:
			if t.Only != nil {
				return []*Exit{t.Only}
			}
			return t.Junction.Exits
		}
		return nil
	}
	for _, j := range g.Junctions {
		for _, e := range j.Exits {
			for _, n := range next(e.To) {
				add(e, n)
			}
		}
	}
	return out
}

// DecisionPairsOf lists the pairs of consecutive decisions of one path, left
// to right, leaving out loop retry edges.
func DecisionPairsOf(p Path) []string {
	var out []string
	var prev *Exit
	for _, s := range p.Steps {
		if s.Junction.Retry == s.Exit {
			continue
		}
		if prev != nil {
			out = append(out, DecisionPair{prev, s.Exit}.Key())
		}
		prev = s.Exit
	}
	return out
}

// StableLabel is the decision's text made unique within its workflow: it stays
// the same when unrelated code moves, so two versions of a workflow can be
// compared decision by decision.
func (j *Junction) StableLabel() string { return j.stable }
