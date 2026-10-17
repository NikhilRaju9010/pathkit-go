package facts

import (
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// DefaultMaxContexts is how many different incoming states of a stage are
// followed (classification.engine.max_contexts).
const DefaultMaxContexts = 16

// Verdict is what the engine says about one stage path.
type Verdict int

const (
	// Possible: in at least one incoming state the stage path can be taken.
	Possible Verdict = iota
	// Impossible: in every incoming state some decision on it cannot be taken.
	Impossible
	// Pending: it cannot be taken in any state examined, but the states are cut
	// off at the limit, so a state that allows it may be among the ones not
	// examined.
	Pending
)

// StageResult is the engine's answer for one stage path.
type StageResult struct {
	Path    model.StagePath
	Verdict Verdict
	// Claim is the proof of the first state examined; Contexts is how many
	// states it was examined in.
	Claim    *Claim
	Contexts int
	// Suspect and Unknown are set when the path is possible and the engine had
	// to forget a value it was asked about twice (possible-correlation), or took
	// a type switch case on a value it cannot follow (unknown-input-type).
	Suspect *Suspect
	Unknown *Suspect
}

// StageReport is the engine's answer for one stage.
type StageReport struct {
	Stage int
	// Results has one entry per stage path; empty for a stage that is too big
	// to list.
	Results []StageResult
	// Contexts is how many different incoming states the stage was examined in;
	// CutOff says there were more (so claims are only pending).
	Contexts int
	CutOff   bool
	// Fresh: the states coming in were not known (an earlier stage was too big
	// to follow), so the stage was examined with nothing known.
	Fresh bool
}

// AnalyzeStages examines the paths of every stage that is small enough, one
// stage at a time. A stage starts with the states the stages before it can
// leave behind (the notebook as it is when the stage's first decision is
// reached, one for every different notebook, at most maxContexts of them). A
// stage path is impossible when it is impossible in every one of them.
func (a *Analyzer) AnalyzeStages(g *model.Graph, stages []model.Stage, maxContexts int) []StageReport {
	if maxContexts < 1 {
		maxContexts = DefaultMaxContexts
	}
	out := make([]StageReport, len(stages))
	contexts := []*Walk{a.NewWalk()}
	cutOff, fresh := false, false
	for k, st := range stages {
		rep := StageReport{Stage: k, Contexts: len(contexts), CutOff: cutOff, Fresh: fresh}
		if st.Big || a.off != "" {
			out[k] = rep
			// Nothing is known about what a stage that is not followed leaves
			// behind: the next one starts empty.
			contexts, cutOff, fresh = []*Walk{a.emptyWalk()}, false, true
			continue
		}
		paths, _ := g.StagePaths(st, int(st.Count.Int64())+1)
		type outcome struct {
			claims  int
			first   *Claim
			allowed bool
			suspect *Suspect
			unknown *Suspect
		}
		res := make([]outcome, len(paths))
		next := map[string]*Walk{}
		var order []string
		for _, ctx := range contexts {
			// Walk the stage paths as a tree: one copy of the notebook per branch.
			a.walkStage(g, st, paths, ctx, func(i int, w *Walk, c *Claim) {
				r := &res[i]
				if c != nil {
					r.claims++
					if r.first == nil {
						r.first = c
					}
					return
				}
				r.allowed = true
				if rr := w.Result(); r.suspect == nil && rr.Suspect != nil {
					r.suspect = rr.Suspect
				}
				if rr := w.Result(); r.unknown == nil && rr.Unknown != nil {
					r.unknown = rr.Unknown
				}
				if paths[i].Continue {
					sig := w.Signature()
					if _, seen := next[sig]; !seen {
						next[sig] = w
						order = append(order, sig)
					}
				}
			})
		}
		for i, p := range paths {
			r := res[i]
			sr := StageResult{Path: p, Contexts: len(contexts), Claim: r.first, Suspect: r.suspect, Unknown: r.unknown}
			switch {
			case r.allowed:
				sr.Verdict = Possible
			case cutOff:
				sr.Verdict = Pending
			default:
				sr.Verdict = Impossible
			}
			rep.Results = append(rep.Results, sr)
		}
		out[k] = rep
		// A set of states that was cut stays cut: the states left out may be
		// what makes a later path possible, so nothing after it may be called
		// impossible until the notebook is emptied (a stage not followed, or
		// no state carried on).
		contexts, fresh = nil, false
		for _, sig := range order {
			if len(contexts) == maxContexts {
				cutOff = true
				break
			}
			w := next[sig]
			w.Rebase()
			contexts = append(contexts, w)
		}
		if len(contexts) == 0 {
			// Every path of the stage that goes on is impossible, or none goes on.
			contexts = []*Walk{a.emptyWalk()}
			fresh, cutOff = true, false
		}
	}
	return out
}

// emptyWalk is a notebook that knows nothing, for a stage whose incoming
// states are not known.
func (a *Analyzer) emptyWalk() *Walk {
	return &Walk{s: newState(a)}
}

// walkStage walks every path of a stage from the notebook ctx, as a tree of
// decisions (the same order and the same "each exit once" rule as
// Graph.StagePaths), and reports for every stage path the walk that ends it,
// or the claim that stops it. paths is the list StagePaths gave.
func (a *Analyzer) walkStage(g *model.Graph, st model.Stage, paths []model.StagePath, ctx *Walk, done func(i int, w *Walk, c *Claim)) {
	if a.off != "" {
		for i := range paths {
			done(i, ctx, nil)
		}
		return
	}
	used := map[model.EdgeID]bool{}
	// index of the stage path a finished road is, in StagePaths order
	idx := 0
	var walk func(t model.Target, w *Walk)
	// skip counts the stage paths below a vetoed exit, to keep idx in step.
	walk = func(t model.Target, w *Walk) {
		if t.Dead() {
			return
		}
		if r := t.Retry; r != nil {
			if used[r.ID] {
				return
			}
			used[r.ID] = true
			if c := w.Step(model.Step{Junction: r.Junction, Exit: r}); c != nil {
				n := stageCount(g, st, r.To, used)
				for i := 0; i < n; i++ {
					done(idx+i, w, c)
				}
				idx += n
			} else {
				walk(r.To, w)
			}
			used[r.ID] = false
			return
		}
		if t.Junction == nil || (st.Next != nil && t.Junction == st.Next) {
			done(idx, w, nil)
			idx++
			return
		}
		exits := t.Junction.Exits
		if t.Only != nil {
			exits = []*model.Exit{t.Only}
		}
		var avail []*model.Exit
		for _, e := range exits {
			if !used[e.ID] {
				avail = append(avail, e)
			}
		}
		for i, e := range avail {
			used[e.ID] = true
			branch := w
			if i < len(avail)-1 {
				branch = w.Clone()
			}
			if c := branch.Step(model.Step{Junction: t.Junction, Exit: e}); c != nil {
				n := stageCount(g, st, e.To, used)
				for k := 0; k < n; k++ {
					done(idx+k, branch, c)
				}
				idx += n
			} else {
				walk(e.To, branch)
			}
			used[e.ID] = false
		}
	}
	walk(st.Entry, ctx.Clone())
}

// stageCount is how many stage paths start from t when the exits in used
// are already taken, counted by listing them (a stage is small).
func stageCount(g *model.Graph, st model.Stage, t model.Target, used map[model.EdgeID]bool) int {
	sub := st
	sub.Entry = t
	// StagePaths starts with nothing used; the only used exits that matter below
	// are the loop heads' own, which t.Retry handling covers through the target.
	list, _ := g.StagePathsFrom(sub, used)
	return len(list)
}
