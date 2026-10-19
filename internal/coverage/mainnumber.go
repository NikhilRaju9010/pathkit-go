package coverage

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// The kinds of main number (docs/DESIGN.md D26), best to worst.
const (
	MainHandler  = "handler"
	MainPath     = "path"
	MainScenario = "scenario"
	MainBranch   = "branch"
)

// MainNumber is the one number a workflow is judged by, and why it is that one.
type MainNumber struct {
	Kind string
	// Covered and Total are the counts behind it. Measured is false when Total
	// is 0: there is nothing to measure, and the workflow does not count as
	// meeting its target.
	Covered, Total int
	Measured       bool
	Percent        float64
	Target         float64
	Met            bool
	// Why is one plain sentence on why this kind was chosen.
	Why string
	// Forced says a person chose the kind (classification.main_number.force);
	// ForcedReason is their reason.
	Forced       bool
	ForcedReason string
}

// HandlerStats is the handler coverage of one workflow (M9.5).
type HandlerStats struct {
	Covered, Total int
	// NotRecorded counts handlers no trace can show; they are left out.
	NotRecorded int
}

// MainOptions are what the config says about main numbers.
type MainOptions struct {
	Targets scope.Targets
	Force   []scope.MainForce
}

func pct(c, t int) float64 {
	if t == 0 {
		return 0
	}
	return float64(int(float64(c)/float64(t)*1000+0.5)) / 10
}

// handlerStats counts the handler branches of a workflow: for every handler it
// registers, the handler itself (1 when a test ran it) and the branches of its
// own code, including the accept and reject of an update validator; and the
// exits of its Selectors (each signal, timer or activity it waits for).
func handlerStats(g *model.Graph, owner string, rows []HandlerResult, seen, unitHit map[string]bool) HandlerStats {
	var hs HandlerStats
	inSelector := map[string]bool{}
	for _, j := range g.AllJunctions() {
		if j.Kind != model.Selector {
			continue
		}
		for _, e := range j.Exits {
			hs.Total++
			if seen[e.ID.String()] || unitHit[e.ID.String()] {
				hs.Covered++
			}
			inSelector[e.Label] = true
		}
	}
	for _, r := range rows {
		if r.Owner != owner {
			continue
		}
		if r.Kind == model.HandlerSignal && inSelector[fmt.Sprintf("signal %q", r.Name)] {
			continue // counted as an exit of the Selector
		}
		if r.Exercised == "not recorded" {
			hs.NotRecorded++
			continue
		}
		hs.Total += 1 + r.Branches
		hs.Covered += r.BranchesTaken
		if r.Exercised == "yes" {
			hs.Covered++
		}
	}
	return hs
}

// mainNumber picks the main number of a workflow by the rules, in order: an
// event-loop workflow by its handlers; one under the path cap by its paths; one
// whose scenarios were counted (and are few enough to list) by its scenarios;
// else by its branches.
func mainNumber(w WorkflowResult, g *model.Graph, hs HandlerStats, policy classify.Policy, opt MainOptions, maxListed int) MainNumber {
	if maxListed <= 0 {
		maxListed = DefaultScenarioMaxListed
	}
	pick := func(kind string) (MainNumber, bool) {
		m := MainNumber{Kind: kind}
		switch kind {
		case MainHandler:
			m.Covered, m.Total = hs.Covered, hs.Total
		case MainPath:
			t := w.Breakdown.TestableCoverage()
			if w.Capped {
				return m, false
			}
			m.Covered, m.Total = t.Covered, t.Paths
		case MainScenario:
			if !w.Scenarios.Measured {
				return m, false
			}
			m.Covered = w.Scenarios.Covered
			tot := w.Scenarios.TestableTotal(policy)
			if !tot.IsInt64() {
				return m, false
			}
			m.Total = int(tot.Int64())
		case MainBranch:
			m.Covered, m.Total = w.AllBranchesTaken, w.AllBranches
		}
		return m, true
	}
	var m MainNumber
	forced := false
	unusable := ""
	for _, f := range opt.Force {
		if f.Workflow == w.Name {
			if mm, ok := pick(f.Kind); ok {
				m, forced = mm, true
				m.Forced, m.ForcedReason = true, f.Reason
				m.Why = fmt.Sprintf("chosen in the config (%s)", f.Reason)
			} else {
				unusable = fmt.Sprintf(" (the config asks for %s coverage, which cannot be used for this workflow)", f.Kind)
			}
		}
	}
	if !forced {
		switch {
		case g != nil && g.EventLoop() && hs.Total > 0:
			m, _ = pick(MainHandler)
			m.Why = "the workflow waits for events in a loop, so what matters is whether tests sent each event and took each handler branch"
		case !w.Capped:
			m, _ = pick(MainPath)
			m.Why = "the workflow has few enough paths to list them all, so path coverage is the number"
		case w.Scenarios.Measured && w.Scenarios.Count.Cmp(big.NewInt(int64(maxListed))) <= 0:
			if mm, ok := pick(MainScenario); ok {
				m = mm
				m.Why = "the workflow has too many paths to list, but only " + w.Scenarios.Count.String() + " different things it can do (scenarios), so scenario coverage is the number"
				break
			}
			fallthrough
		default:
			m, _ = pick(MainBranch)
			m.Why = "the workflow has too many paths and scenarios to count one by one, so branch coverage (every decision taken both ways) is the number"
			if !w.Scenarios.Measured && w.Scenarios.Why != "" {
				m.Why += " (scenarios not measured: " + w.Scenarios.Why + ")"
			}
		}
	}
	m.Why += unusable
	m.Measured = m.Total > 0
	m.Percent = pct(m.Covered, m.Total)
	m.Target = opt.Targets.For(m.Kind)
	m.Met = m.Measured && m.Percent >= m.Target
	return m
}

// MainHeadline is the project's first line: how many workflows meet their
// target, and branch coverage over all of them.
type MainHeadline struct {
	Met, Total int
	// NotMeasured are the workflows with nothing to measure; they are not
	// counted as meeting their target.
	NotMeasured []string
	// Target is the default target; PerKind says whether any kind has its own.
	Target  float64
	PerKind bool
	// BranchCovered and BranchTotal are over every workflow's whole map.
	BranchCovered, BranchTotal int
}

// MainHeadline computes the headline of a run.
func (r Result) MainHeadline(t scope.Targets) MainHeadline {
	h := MainHeadline{Target: t.For(""), PerKind: len(t.ByKind) > 0}
	for _, w := range r.Workflows {
		h.Total++
		switch {
		case !w.Main.Measured:
			h.NotMeasured = append(h.NotMeasured, w.Name)
		case w.Main.Met:
			h.Met++
		}
		h.BranchCovered += w.AllBranchesTaken
		h.BranchTotal += w.AllBranches
	}
	sort.Strings(h.NotMeasured)
	return h
}
