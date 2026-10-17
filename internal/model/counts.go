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

// counter counts paths from a target, optionally stopping at a junction: a
// road that reaches stop counts as one path that continues there.
type counter struct {
	g    *Graph
	stop *Junction
	memo map[countKey]*big.Int
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
			if e.Label == "exit" {
				n = c.count(e.To)
			}
		}
	case t.Junction != nil:
		if t.Junction == c.stop {
			n = one
			break
		}
		n = new(big.Int)
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*Exit{t.Only}
		}
		for _, e := range exits {
			n = new(big.Int).Add(n, c.count(e.To))
		}
	default:
		n = one
	}
	c.memo[k] = n
	return n
}
