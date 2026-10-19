package model

import (
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// DefaultScenarioMaxStates is how many states the scenario automaton may have
// before PathKit gives up on a workflow (classification.scenarios.max_states).
const DefaultScenarioMaxStates = 100000

// ScenarioSet is the scenarios of one workflow, counted without listing paths
// (docs/DESIGN.md D25). A scenario is one distinct list of external steps (see
// Fingerprint). Several paths can make the same steps.
type ScenarioSet struct {
	// Count is the exact number of scenarios.
	Count *big.Int
	// Listed are the steps of up to the asked number of scenarios, in a stable order.
	Listed [][]string
	// Truncated: Count is more than len(Listed).
	Truncated bool
	// Unmeasured, when not empty, is why no count was made.
	Unmeasured string
}

// nfa is the map as a machine that reads external steps: a state is a target
// of the map, an edge is an exit (with the steps its road makes), and an exit
// that makes no step is an empty move.
type nfa struct {
	n     int
	edges [][]nedge // by state
	acc   int       // the one accepting state
	start int
}

type nedge struct {
	sym string // "" is an empty move
	to  int
}

// MeasureScenarios counts the scenarios from the shape of the map: it builds
// the machine, makes it deterministic and counts the words it accepts. The
// listing is not used, so it works for workflows with any number of paths.
// A workflow with a place PathKit did not read is not measured: its paths
// cannot be told from each other by their steps.
func (g *Graph) MeasureScenarios(maxListed, maxStates int) ScenarioSet {
	if maxStates <= 0 {
		maxStates = DefaultScenarioMaxStates
	}
	m, why := g.buildNFA(maxStates)
	if why != "" {
		return ScenarioSet{Count: new(big.Int), Unmeasured: why}
	}
	d, why := m.determinize(maxStates)
	if why != "" {
		return ScenarioSet{Count: new(big.Int), Unmeasured: why}
	}
	return d.words(maxListed)
}

func (g *Graph) buildNFA(limit int) (*nfa, string) {
	m := &nfa{}
	newState := func() int {
		m.edges = append(m.edges, nil)
		m.n++
		return m.n - 1
	}
	m.acc = newState()
	state := map[countKey]int{}
	unknown := false
	// emit adds the steps as a chain of edges from -> to.
	emit := func(from int, steps []string, to int) {
		cur := from
		for i, s := range steps {
			next := to
			if i < len(steps)-1 {
				next = newState()
			}
			m.edges[cur] = append(m.edges[cur], nedge{sym: s, to: next})
			cur = next
		}
		if len(steps) == 0 {
			m.edges[from] = append(m.edges[from], nedge{to: to})
		}
	}
	road := func(t Target) []string {
		s, u := g.roadSteps(t)
		unknown = unknown || u
		return s
	}
	// walk makes the state of a target (and everything below it) and returns it; -1 for none.
	var walk func(t Target) int
	walk = func(t Target) int {
		if t.Dead() {
			return -1
		}
		k := keyOf(t)
		if s, ok := state[k]; ok {
			return s
		}
		s := newState()
		state[k] = s
		switch {
		case t.Retry != nil:
			// the head is met again with its iterate used up: only its exit is left
			head := t.Retry.Junction
			for _, e := range head.Exits {
				if e.Label != "exit" {
					continue
				}
				if to := walk(e.To); to >= 0 {
					steps := append(append([]string(nil), road(t.Retry.To)...), road(e.To)...)
					emit(s, steps, to)
				}
			}
		case t.Junction == nil:
			emit(s, []string{endLabel(t.End)}, m.acc)
		default:
			exits := t.Junction.Exits
			if t.Only != nil {
				exits = []*Exit{t.Only}
			}
			for _, e := range exits {
				to := walk(e.To)
				if to < 0 {
					continue
				}
				steps := append(g.stepLabel(t.Junction, e, false), road(e.To)...)
				emit(s, steps, to)
			}
		}
		return s
	}
	s0 := walk(g.Start)
	startSteps := road(g.Start)
	if unknown {
		return nil, "the workflow has places PathKit could not read, so its paths cannot be told apart by their steps"
	}
	m.start = newState()
	if s0 >= 0 {
		emit(m.start, startSteps, s0)
	}
	if m.n > limit*20 {
		return nil, "the machine of its steps is too big (" + strconv.Itoa(m.n) + " states)"
	}
	return m, ""
}

// dfa is the deterministic machine: sets of nfa states.
type dfa struct {
	next []map[string]int
	acc  []bool
}

// closure adds every state reachable by empty moves.
func (m *nfa) closure(set []int) []int {
	seen := map[int]bool{}
	var out []int
	var add func(x int)
	add = func(x int) {
		if seen[x] {
			return
		}
		seen[x] = true
		out = append(out, x)
		for _, e := range m.edges[x] {
			if e.sym == "" {
				add(e.to)
			}
		}
	}
	for _, x := range set {
		add(x)
	}
	sort.Ints(out)
	return out
}

func setKey(set []int) string {
	var b strings.Builder
	for _, x := range set {
		b.WriteString(strconv.Itoa(x))
		b.WriteByte(',')
	}
	return b.String()
}

func (m *nfa) determinize(limit int) (*dfa, string) {
	d := &dfa{}
	index := map[string]int{}
	var sets [][]int
	add := func(set []int) int {
		k := setKey(set)
		if i, ok := index[k]; ok {
			return i
		}
		index[k] = len(sets)
		sets = append(sets, set)
		d.next = append(d.next, map[string]int{})
		acc := false
		for _, x := range set {
			acc = acc || x == m.acc
		}
		d.acc = append(d.acc, acc)
		return len(sets) - 1
	}
	add(m.closure([]int{m.start}))
	for i := 0; i < len(sets); i++ {
		if len(sets) > limit {
			return nil, "more than " + strconv.Itoa(limit) + " distinct states of external steps (too complex)"
		}
		by := map[string][]int{}
		for _, x := range sets[i] {
			for _, e := range m.edges[x] {
				if e.sym != "" {
					by[e.sym] = append(by[e.sym], e.to)
				}
			}
		}
		for sym, tos := range by {
			d.next[i][sym] = add(m.closure(tos))
		}
	}
	return d, ""
}

// words counts the accepted words and lists up to maxListed of them.
func (d *dfa) words(maxListed int) ScenarioSet {
	memo := map[int]*big.Int{}
	var count func(s int) *big.Int
	count = func(s int) *big.Int {
		if v, ok := memo[s]; ok {
			return v
		}
		n := new(big.Int)
		if d.acc[s] {
			n.SetInt64(1)
		}
		for _, to := range d.next[s] {
			n.Add(n, count(to))
		}
		memo[s] = n
		return n
	}
	out := ScenarioSet{Count: new(big.Int).Set(count(0))}
	var cur []string
	var walk func(s int)
	walk = func(s int) {
		if len(out.Listed) >= maxListed {
			return
		}
		if d.acc[s] {
			out.Listed = append(out.Listed, append([]string(nil), cur...))
		}
		syms := make([]string, 0, len(d.next[s]))
		for k := range d.next[s] {
			syms = append(syms, k)
		}
		sort.Strings(syms)
		for _, k := range syms {
			cur = append(cur, k)
			walk(d.next[s][k])
			cur = cur[:len(cur)-1]
		}
	}
	walk(0)
	out.Truncated = out.Count.Cmp(big.NewInt(int64(len(out.Listed)))) > 0
	return out
}
