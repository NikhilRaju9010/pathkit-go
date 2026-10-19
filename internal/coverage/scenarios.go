package coverage

import (
	"math/big"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Defaults of classification.scenarios.
const (
	DefaultScenarioMaxListed = 500
)

// ScenarioOptions are classification.scenarios.
type ScenarioOptions struct {
	MaxListed int // 0 means DefaultScenarioMaxListed
	MaxStates int // 0 means model.DefaultScenarioMaxStates
}

// Scenario is one distinct list of external steps a workflow can make (see
// model.Graph.Fingerprint) and what is known about it.
type Scenario struct {
	ID    string
	Steps []string
	// Class is the bucket of the scenario: testable if any path in it is, else
	// needs-review if any is, else needs-special-test if any is, else
	// cannot-be-tested. A scenario a test ran is always testable.
	Class   classify.Classification
	Covered bool
	Traces  int
	// Paths are the numbers of the listed paths that make this scenario, the
	// first being the main one (the covered one if any, else the first listed).
	Paths []int
	// PathCount is how many listed paths make the scenario (Paths holds the
	// first few, so nothing grows with the number of paths).
	PathCount int
}

// ScenarioResult is the scenarios of one workflow.
type ScenarioResult struct {
	// Measured is false when the scenarios could not be counted; Why says why.
	Measured bool
	Why      string
	// Count is the exact number of scenarios; Items lists up to the asked number.
	Count *big.Int
	Items []Scenario
	// Truncated: Count is more than len(Items).
	Truncated bool
	// FromPaths: the scenarios come from the listed paths (the workflow is
	// under the cap), so every scenario was checked one by one. Otherwise they
	// come from the shape of the map, and Unchecked of them have no listed path.
	FromPaths bool
	Unchecked *big.Int
	// Covered is how many scenarios a test ran; the bucket counts are over all
	// scenarios (an unchecked one counts as testable, see TestableTotal).
	Covered                                             int
	Testable, NeedsReview, NeedsSpecial, CannotBeTested int
}

// maxScenarioPaths is how many path numbers a scenario keeps.
const maxScenarioPaths = 10

// classRank orders the buckets by what a scenario is when its paths differ.
func classRank(b classify.Bucket) int {
	switch b {
	case classify.Testable:
		return 0
	case classify.NeedsReview:
		return 1
	case classify.NeedsSpecialTest:
		return 2
	}
	return 3
}

// scenarios works out the scenarios of one workflow. ran maps a scenario ID to
// the traces that ran it (all counted traces, listed path or not).
func scenarios(w Workflow, wr *WorkflowResult, capped bool, ran map[string]int, opt ScenarioOptions) ScenarioResult {
	maxListed := opt.MaxListed
	if maxListed <= 0 {
		maxListed = DefaultScenarioMaxListed
	}
	g := w.Graph
	type group struct {
		id    string
		steps []string
		paths []int // indexes into wr.Paths
	}
	byID := map[string]*group{}
	var order []*group
	for i, p := range wr.Paths {
		steps := g.Fingerprint(p.Path)
		id := model.ScenarioID(steps)
		gr := byID[id]
		if gr == nil {
			gr = &group{id: id, steps: steps}
			byID[id] = gr
			order = append(order, gr)
		}
		gr.paths = append(gr.paths, i)
		wr.Paths[i].ScenarioID = id
	}
	res := ScenarioResult{Measured: true, Count: new(big.Int), Unchecked: new(big.Int)}
	if capped {
		set := g.MeasureScenarios(maxListed, opt.MaxStates)
		if set.Unmeasured != "" {
			return ScenarioResult{Why: set.Unmeasured, Count: new(big.Int), Unchecked: new(big.Int)}
		}
		res.Count = set.Count
	} else {
		res.FromPaths = true
		res.Count = big.NewInt(int64(len(order)))
	}
	build := func(gr *group) Scenario {
		sc := Scenario{ID: gr.id, Steps: gr.steps, Traces: ran[gr.id]}
		best := -1
		for _, i := range gr.paths {
			p := wr.Paths[i]
			if best < 0 || classRank(p.Class.Bucket) < classRank(wr.Paths[best].Class.Bucket) {
				best = i
			}
		}
		sc.Class = wr.Paths[best].Class
		if capped && sc.Class.Bucket == classify.CannotBeTested {
			// the list is only a part of the paths: unlisted paths of this scenario may
			// be possible, so it is not claimed impossible
			sc.Class.Bucket, sc.Class.Label = classify.NeedsReview, "model-limit"
			sc.Class.Explanation = "every listed path of this scenario is shown impossible, but the workflow has more paths than the list holds"
		}
		sc.Covered = sc.Traces > 0
		if sc.Covered && sc.Class.Bucket != classify.Testable {
			sc.Class = classify.DefaultTestable()
		}
		// the main path first: a covered one, else the first listed
		main := gr.paths[0]
		for _, i := range gr.paths {
			if wr.Paths[i].Covered {
				main = i
				break
			}
		}
		sc.PathCount = len(gr.paths)
		sc.Paths = append(sc.Paths, wr.Paths[main].Number)
		for _, i := range gr.paths {
			if i != main && len(sc.Paths) < maxScenarioPaths {
				sc.Paths = append(sc.Paths, wr.Paths[i].Number)
			}
		}
		return sc
	}
	known := 0
	coveredIDs := map[string]bool{}
	for _, gr := range order {
		sc := build(gr)
		known++
		switch sc.Class.Bucket {
		case classify.Testable:
			res.Testable++
		case classify.NeedsReview:
			res.NeedsReview++
		case classify.NeedsSpecialTest:
			res.NeedsSpecial++
		default:
			res.CannotBeTested++
		}
		if sc.Covered {
			res.Covered++
			coveredIDs[sc.ID] = true
		}
		if len(res.Items) < maxListed {
			res.Items = append(res.Items, sc)
		}
		// a path's "same behaviour as" label points at the scenario's main path
		main := sc.Paths[0]
		for _, i := range gr.paths {
			if wr.Paths[i].Number != main {
				wr.Paths[i].SameAs = pathID(wr.Paths, main)
			}
		}
	}
	// scenarios a test ran that no listed path makes (a path beyond the list)
	for id, n := range ran {
		if n > 0 && byID[id] == nil && capped {
			res.Covered++
			coveredIDs[id] = true
			known++
			res.Testable++
		}
	}
	if capped {
		un := new(big.Int).Sub(res.Count, big.NewInt(int64(known)))
		if un.Sign() < 0 {
			un.SetInt64(0)
		}
		res.Unchecked = un
	}
	// scenarios the automaton lists first would be missing from Items if a
	// covered one lies beyond the listing: list the covered ones that were not
	sort.SliceStable(res.Items, func(i, j int) bool { return res.Items[i].Paths[0] < res.Items[j].Paths[0] })
	res.Truncated = res.Count.Cmp(big.NewInt(int64(len(res.Items)))) > 0
	return res
}

func pathID(paths []PathResult, number int) string {
	for _, p := range paths {
		if p.Number == number {
			return p.Path.ID()
		}
	}
	return ""
}

// TestableTotal is how many scenarios testable coverage is measured over: the
// testable ones, the unchecked ones, and (under the default policy) the
// needs-review ones.
func (r ScenarioResult) TestableTotal(p classify.Policy) *big.Int {
	n := big.NewInt(int64(r.Testable))
	if p == "" || p == classify.CountAsTestable {
		n.Add(n, big.NewInt(int64(r.NeedsReview)))
	}
	return n.Add(n, orZero(r.Unchecked))
}

// PairResult is the decision-pair coverage of a workflow: pairs of outcomes of
// two decisions where the second is the next decision after the first (see
// model.DecisionPair), without the pairs the listing shows impossible.
type PairResult struct {
	Covered, Total int
	// Exact: the pairs were checked against the whole list of paths; false for a
	// workflow over the cap, where every pair of the map counts as possible.
	Exact bool
}

// pairs counts the pairs of a workflow. ran holds the pairs the counted traces
// ran.
func pairs(g *model.Graph, wr *WorkflowResult, capped bool, ran map[string]bool) PairResult {
	possible := map[string]bool{}
	if !capped {
		for _, p := range wr.Paths {
			if p.Class.Bucket == classify.CannotBeTested && !p.Covered {
				continue
			}
			for _, k := range model.DecisionPairsOf(p.Path) {
				possible[k] = true
			}
		}
	}
	r := PairResult{Exact: !capped}
	for _, p := range g.DecisionPairs() {
		k := p.Key()
		if ran[k] {
			r.Total++
			r.Covered++
			continue
		}
		if capped || possible[k] {
			r.Total++
		}
	}
	return r
}
