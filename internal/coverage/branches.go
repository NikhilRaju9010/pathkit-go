package coverage

import (
	"fmt"
	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"sort"
)

// The state of one decision outcome ("branch").
const (
	// OutcomeCovered: a test took it.
	OutcomeCovered = "covered"
	// OutcomeMissed: it can happen (or PathKit cannot show it cannot) and no test took it.
	OutcomeMissed = "missed"
	// OutcomeImpossible: every path through it is shown to be impossible. It is
	// not in the total ("Can't happen").
	OutcomeImpossible = "impossible"
	// OutcomeBlocked: every path through it is impossible or a known bug, and at
	// least one is a known bug. It stays in the total, on a line of its own.
	OutcomeBlocked = "blocked"
)

// OutcomeResult is one decision outcome of a workflow (see model.Branch) and
// what the tests did with it. A helper's outcome is one result, however many
// call sites copy it.
type OutcomeResult struct {
	ID       string
	File     string // as printed (the caller's Display)
	Line     int
	Func     string
	Decision string
	Outcome  string
	Kind     model.JunctionKind
	Helper   bool
	// Cleanup: the decision runs under a disconnected context, the cleanup after a
	// cancel; reports show these as a section of their own.
	Cleanup bool
	Status  string
	// Call and FailureKind: for the outcome of a call (an activity, child workflow
	// or Nexus call), its name, and the kind of failure this exit takes ("timeout")
	// when the code tells kinds apart; "" otherwise.
	Call        string
	FailureKind string
	// Tests are the Go tests whose counted runs took the outcome; Unnamed says some
	// run that took it did not say which test it was (parallel tests). Flip is why
	// the decision check cannot force the outcome ("" when it can).
	Tests   []string
	Unnamed bool
	Flip    string
	// Label and Why are, for an impossible or blocked outcome, the label of the
	// claim and its explanation; Proof is the reasoning.
	Label string
	Why   string
	Proof []classify.ProofStep
	// Exits are the exit IDs of the outcome (several for a helper copied in).
	Exits []string
	// Sites is how many copies of a helper's decision the map has (1 otherwise);
	// SitesTaken how many a test took.
	Sites, SitesTaken int
	// MissedPaths are the listed paths through the outcome that no test ran, and
	// MissedScenarios their distinct scenarios.
	MissedPaths     int
	MissedScenarios int
}

// BranchSummary counts decision outcomes.
type BranchSummary struct {
	// Covered and Possible: Possible is every outcome that can happen, so the
	// ones shown impossible are left out of it; blocked ones are in it.
	Covered, Possible, Impossible, Blocked int
}

// Percent is covered over possible, to one decimal; 100 when nothing can happen.
func (b BranchSummary) Percent() float64 { return Percent(b.Covered, b.Possible) }

// Add sums two summaries.
func (b BranchSummary) Add(o BranchSummary) BranchSummary {
	return BranchSummary{b.Covered + o.Covered, b.Possible + o.Possible, b.Impossible + o.Impossible, b.Blocked + o.Blocked}
}

// Missed is how many outcomes that can happen no test took.
func (b BranchSummary) Missed() int { return b.Possible - b.Covered }

// exitTally counts the paths (and pruned groups) through one exit by what they
// are; cannotC and knownC are the first claim of each kind, for the proof.
type exitTally struct {
	n, cannot, known, other int
	cannotC, knownC         *classify.Classification
}

// outcomes works out the state of every decision outcome of a workflow, after
// its paths are classified. An outcome is impossible only when the whole map
// is known (the list and the pruned parts add up to every path) and every path
// through it is cannot-be-tested; it is blocked when every path through it is
// cannot-be-tested or a known bug and one is a known bug. Anything unknown
// stays possible, and an outcome a test took is always possible.
func outcomes(g *model.Graph, wr *WorkflowResult, pruned []classify.PrunedClaim, capped bool, seen, unitHit, touched map[string]bool, exitTests map[string]map[string]bool, exitUnnamed map[string]bool, display func(string) string) ([]OutcomeResult, BranchSummary) {
	tally := map[string]*exitTally{}
	at := func(id string) *exitTally {
		t := tally[id]
		if t == nil {
			t = &exitTally{}
			tally[id] = t
		}
		return t
	}
	type pathRef struct {
		scenario string
		covered  bool
	}
	through := map[string][]pathRef{}
	mark := func(id string, c *classify.Classification) {
		t := at(id)
		t.n++
		switch {
		case c.Bucket == classify.CannotBeTested:
			t.cannot++
			if t.cannotC == nil {
				t.cannotC = c
			}
		case c.Label == classify.KnownDefect:
			t.known++
			if t.knownC == nil {
				t.knownC = c
			}
		default:
			t.other++
		}
	}
	for i := range wr.Paths {
		pr := &wr.Paths[i]
		for _, s := range pr.Path.Steps {
			id := s.Exit.ID.String()
			mark(id, &pr.Class)
			through[id] = append(through[id], pathRef{pr.ScenarioID, pr.Covered})
		}
	}
	reach := map[*model.Exit][]*model.Exit{} // below the last exit of a pruned group; many groups end alike
	for gi, pg := range pruned {
		class := pg.Class
		if gi < len(wr.Pruned) && wr.Pruned[gi].Unpruned {
			class = wr.Pruned[gi].Class
		}
		if len(pg.Group.Prefix) == 0 {
			continue
		}
		last := pg.Group.Prefix[len(pg.Group.Prefix)-1].Exit
		for _, s := range pg.Group.Prefix {
			mark(s.Exit.ID.String(), &class)
		}
		below, ok := reach[last]
		if !ok {
			below = g.ReachableFrom(last.To)
			reach[last] = below
		}
		for _, e := range below {
			mark(e.ID.String(), &class)
		}
	}

	kinds := map[*model.Exit]model.FailureKind{}
	names := map[*model.Junction]string{}
	for _, cs := range g.CallSites() {
		names[cs.Junction] = cs.Name
		for _, f := range cs.Failures {
			kinds[f.Exit] = f
			names[f.Exit.Junction] = cs.Name
		}
	}
	var out []OutcomeResult
	var sum BranchSummary
	for _, b := range g.Branches() {
		r := OutcomeResult{ID: b.ID, File: b.File, Line: b.Line, Func: b.Func, Decision: b.Decision, Outcome: b.Outcome, Kind: b.Kind, Helper: b.Helper, Cleanup: b.Cleanup, Sites: len(b.Exits)}
		if display != nil && len(b.Exits) > 0 && b.Exits[0].Junction.Pos.Filename != "" {
			r.File = display(b.Exits[0].Junction.Pos.Filename)
		}
		if len(b.Exits) > 0 {
			r.Call = names[b.Exits[0].Junction]
			r.FailureKind = kinds[b.Exits[0]].Kind
		}
		r.Flip = g.FlipReason(b)
		tests := map[string]bool{}
		var agg exitTally
		ranOnce := false // a test that did not count (unfinished, fits no path) ran it
		scen := map[string]bool{}
		for _, e := range b.Exits {
			id := e.ID.String()
			r.Exits = append(r.Exits, id)
			if seen[id] || unitHit[id] {
				r.SitesTaken++
			}
			for n := range exitTests[id] {
				tests[n] = true
			}
			if exitUnnamed[id] {
				r.Unnamed = true
			}
			if touched[id] {
				ranOnce = true
			}
			if t := tally[id]; t != nil {
				agg.n += t.n
				agg.cannot += t.cannot
				agg.known += t.known
				agg.other += t.other
			}
			for _, p := range through[id] {
				if !p.covered {
					r.MissedPaths++
					if p.scenario != "" {
						scen[p.scenario] = true
					}
				}
			}
		}
		r.MissedScenarios = len(scen)
		for n := range tests {
			r.Tests = append(r.Tests, n)
		}
		sort.Strings(r.Tests)
		switch {
		case r.SitesTaken > 0:
			r.Status = OutcomeCovered
		case capped || agg.n == 0 || agg.other > 0 || ranOnce:
			r.Status = OutcomeMissed
		case agg.known > 0:
			r.Status = OutcomeBlocked
		default:
			r.Status = OutcomeImpossible
		}
		if r.Status == OutcomeImpossible || r.Status == OutcomeBlocked {
			r.Label, r.Why, r.Proof = firstClaim(tally, b, r.Status == OutcomeBlocked)
		}
		switch r.Status {
		case OutcomeImpossible:
			sum.Impossible++
		case OutcomeCovered:
			sum.Possible++
			sum.Covered++
		case OutcomeBlocked:
			sum.Possible++
			sum.Blocked++
		default:
			sum.Possible++
		}
		out = append(out, r)
	}
	return out, sum
}

// firstClaim is the label, explanation and proof of the claim behind an
// impossible or blocked outcome: a known bug first for a blocked one, else the
// first impossible path (or pruned group) through it.
func firstClaim(tally map[string]*exitTally, b *model.Branch, blocked bool) (string, string, []classify.ProofStep) {
	var cannot, known *classify.Classification
	for _, e := range b.Exits {
		if t := tally[e.ID.String()]; t != nil {
			if cannot == nil {
				cannot = t.cannotC
			}
			if known == nil {
				known = t.knownC
			}
		}
	}
	c := cannot
	if blocked && known != nil {
		c = known
	}
	if c == nil {
		c = known
	}
	if c == nil {
		return "", "", nil
	}
	return c.Label, c.Explanation, c.Proof
}

// ConditionPart is one atomic part of an && / || condition and whether a test
// ever saw it true and ever saw it false (when it was evaluated).
type ConditionPart struct {
	ID       string
	Text     string
	Decision string
	File     string
	Line     int
	Func     string
	True     bool
	False    bool
}

// ConditionSummary counts condition parts: every part has two values to see,
// so Total is twice the parts. NotRecorded says no counted trace was written by
// a recorder that records parts (an old trace), so nothing can be said.
type ConditionSummary struct {
	Covered, Total int
	NotRecorded    bool
	Parts          []ConditionPart
}

// Percent is covered over total, to one decimal.
func (c ConditionSummary) Percent() float64 { return Percent(c.Covered, c.Total) }

func (c ConditionSummary) add(o ConditionSummary) ConditionSummary {
	return ConditionSummary{Covered: c.Covered + o.Covered, Total: c.Total + o.Total, NotRecorded: c.NotRecorded || o.NotRecorded}
}

func conditionResults(g *model.Graph, seen map[string]bool, recorded bool, display func(string) string) ConditionSummary {
	var out ConditionSummary
	for _, c := range g.Conditions() {
		p := ConditionPart{ID: c.ID, Text: c.Text, Decision: c.Decision.Label, File: c.File, Line: c.Line, Func: c.Func, True: seen[c.ID+":t"], False: seen[c.ID+":f"]}
		if display != nil && c.Decision.Pos.Filename != "" {
			p.File = display(c.Decision.Pos.Filename)
		}
		out.Parts = append(out.Parts, p)
		out.Total += 2
		if p.True {
			out.Covered++
		}
		if p.False {
			out.Covered++
		}
	}
	out.NotRecorded = out.Total > 0 && !recorded
	return out
}

// HelperRegion is a helper function whose decisions the workflow's paths repeat:
// a candidate to test on its own (milestone M11, O3). Advice only: nothing in
// any number depends on it.
type HelperRegion struct {
	Name      string
	File      string // as printed
	Line      int
	Decisions int // decisions written in the helper
	Sites     int // places that call it
}

// Score is how much the helper multiplies the paths: its decisions times the
// places that call it.
func (h HelperRegion) Score() int { return h.Decisions * h.Sites }

// helperRegions lists the helpers of a workflow with decisions, by what they
// multiply, most first. A helper copied in once appears once (with 1 site).
func helperRegions(g *model.Graph, units []UnitResult, display func(string) string) []HelperRegion {
	type key struct {
		file string
		line int
	}
	decs := map[key]map[string]bool{} // helper declaration -> distinct decisions (by position)
	sites := map[key]map[string]bool{}
	names := map[key]string{}
	for _, j := range g.Junctions {
		s := j.Site
		if s == nil || s.Decl == nil {
			continue
		}
		pos := g.FileSet().Position(s.Decl.Name.Pos())
		k := key{pos.Filename, pos.Line}
		if decs[k] == nil {
			decs[k], sites[k] = map[string]bool{}, map[string]bool{}
			names[k] = s.Decl.Name.Name
		}
		decs[k][fmt.Sprintf("%s:%d:%d", j.Pos.Filename, j.Pos.Line, j.Pos.Column)] = true
		sites[k][s.ID] = true
	}
	var out []HelperRegion
	for k, d := range decs {
		file := k.file
		if display != nil {
			file = display(file)
		}
		out = append(out, HelperRegion{Name: names[k], File: file, Line: k.line, Decisions: len(d), Sites: len(sites[k])})
	}
	for _, u := range units {
		if u.Branches >= 2 {
			out = append(out, HelperRegion{Name: u.Name, File: u.File, Line: u.Line, Decisions: u.Branches / 2, Sites: u.Sites})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score() != out[j].Score() {
			return out[i].Score() > out[j].Score()
		}
		return out[i].Name < out[j].Name
	})
	return out
}
