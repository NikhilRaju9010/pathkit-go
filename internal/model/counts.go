package model

import "math/big"

// PathCount is the exact number of paths the workflow has, counted the way
// Paths lists them (each exit, and each loop's retry edge, at most once per
// path), without listing them. A workflow with a hundred ifs in a row has
// 2^100 paths; this still answers at once.
//
// A road's count depends only on where it leads, because a path meets every
// junction at most once, except that a loop's head is met again after its
// body (through the retry edge), and then only the loop's "exit" is left (its
// "iterate" and "retry" are used up). So the count of a target is memoized.
func (g *Graph) PathCount() *big.Int {
	return newCounter(g, nil).count(g.Start)
}

// OldVersionPathCount is the exact number of paths that take the old side of
// some workflow.GetVersion check (a side kept only so old histories replay),
// and whether the workflow has such a check at all.
func (g *Graph) OldVersionPathCount() (n *big.Int, has bool) {
	old := map[*Exit]bool{}
	for _, j := range g.Junctions {
		for _, f := range j.Facts {
			if f.Kind != VersionCheck {
				continue
			}
			for _, e := range j.Exits {
				if e.Label == f.Exit {
					old[e] = true
					has = true
				}
			}
		}
	}
	if !has {
		return new(big.Int), false
	}
	c := newCounter(g, nil)
	c.skip = old
	return new(big.Int).Sub(g.PathCount(), c.count(g.Start)), true
}

// counter counts paths from a target, optionally stopping at a junction: a
// road that reaches stop counts as one path that continues there.
type counter struct {
	g    *Graph
	stop *Junction
	// skip are exits the count does not take.
	skip map[*Exit]bool
	// outside, when set, makes a road that reaches a decision not in it count as
	// one path that continues there (a part of a stage ends at the first decision
	// outside it).
	outside map[*Junction]bool
	memo    map[countKey]*big.Int
}

// countKey is a target: a junction (with the exit it is limited to, if any), a
// loop's retry edge, or an end.
type countKey struct {
	j    *Junction
	only *Exit
	r    *Exit
	end  EndKind
}

func newCounter(g *Graph, stop *Junction) *counter {
	return &counter{g: g, stop: stop, memo: map[countKey]*big.Int{}}
}

func keyOf(t Target) countKey {
	switch {
	case t.Retry != nil:
		return countKey{r: t.Retry}
	case t.Junction != nil:
		return countKey{j: t.Junction, only: t.Only}
	}
	return countKey{end: t.End}
}

var one = big.NewInt(1)

func (c *counter) count(t Target) *big.Int {
	if t.Dead() {
		return new(big.Int)
	}
	k := keyOf(t)
	if v, ok := c.memo[k]; ok {
		return v
	}
	var n *big.Int
	switch {
	case t.Retry != nil:
		// After the retry edge the head is met again; its iterate and retry are
		// used, so only its exit can be taken (a "for {}" has none: no path).
		n = new(big.Int)
		for _, e := range t.Retry.Junction.Exits {
			if e.Label == "exit" && !c.skip[e] {
				n = c.count(e.To)
			}
		}
	case t.Junction != nil:
		if t.Junction == c.stop || (c.outside != nil && !c.outside[t.Junction]) {
			n = one
			break
		}
		n = new(big.Int)
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*Exit{t.Only}
		}
		for _, e := range exits {
			if c.skip[e] {
				continue
			}
			n = new(big.Int).Add(n, c.count(e.To))
		}
	default:
		n = one
	}
	c.memo[k] = n
	return n
}

func bigInt(n int) *big.Int { return big.NewInt(int64(n)) }
