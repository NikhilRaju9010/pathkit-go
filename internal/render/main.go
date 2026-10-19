package render

import (
	"fmt"
	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"math/big"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// kindName is how a kind of main number is written in a sentence.
var kindName = map[string]string{
	coverage.MainHandler:  "handler coverage",
	coverage.MainPath:     "path coverage",
	coverage.MainScenario: "scenario coverage",
	coverage.MainBranch:   "branch coverage",
}

// MainHeadlineLines are the first lines of every text report and of the page:
// how many workflows meet their target, and branch coverage over all of them.
func MainHeadlineLines(res coverage.Result) []string {
	if len(res.Workflows) == 0 {
		return nil
	}
	h := res.MainHeadline(res.Targets)
	target := "main number ≥ " + Threshold(h.Target)
	if h.PerKind {
		var parts []string
		for _, k := range []string{coverage.MainPath, coverage.MainScenario, coverage.MainBranch, coverage.MainHandler} {
			if v, ok := res.Targets.ByKind[k]; ok {
				parts = append(parts, k+" "+Threshold(v))
			}
		}
		target += " · " + strings.Join(parts, ", ")
	}
	lines := []string{
		fmt.Sprintf("%d of %d workflows meet their target (%s)", h.Met, h.Total, target),
		"Branch coverage (all workflows): " + Ratio(h.BranchCovered, h.BranchTotal, nil, "no branches"),
	}
	pc, pt := 0, 0
	for _, w := range res.Workflows {
		pc += w.Pairs.Covered
		pt += w.Pairs.Total
	}
	if pt > 0 {
		lines = append(lines, "Decision pairs (all workflows): "+Ratio(pc, pt, nil, "no pairs")+" — an outcome of one decision followed by an outcome of the next")
	}
	if len(h.NotMeasured) > 0 {
		lines = append(lines, "Not measured, so not counted as meeting their target: "+strings.Join(h.NotMeasured, ", "))
	}
	return lines
}

// MainHeadlineBlock is MainHeadlineLines followed by a blank line ("" when
// there is nothing to say).
func MainHeadlineBlock(res coverage.Result) string {
	lines := MainHeadlineLines(res)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// MainLine is a workflow's main number in one line, with its target.
func MainLine(w coverage.WorkflowResult) string {
	m := w.Main
	if !m.Measured {
		return fmt.Sprintf("Main number: %s not measured (nothing to measure); %s", kindName[m.Kind], m.Why)
	}
	verdict := "target " + Threshold(m.Target) + " met"
	if !m.Met {
		verdict = "target " + Threshold(m.Target) + " not met"
	}
	return fmt.Sprintf("Main number: %s %d/%d (%s) · %s", kindName[m.Kind], m.Covered, m.Total, Pct(m.Percent, nil), verdict)
}

// MainShort is a workflow's main number on a line of the summary.
func MainShort(w coverage.WorkflowResult) string {
	m := w.Main
	if !m.Measured {
		return kindName[m.Kind] + " not measured"
	}
	verdict := "target " + Threshold(m.Target) + " met"
	if !m.Met {
		verdict = "target " + Threshold(m.Target) + " not met"
	}
	return fmt.Sprintf("%s %d/%d (%s), %s", kindName[m.Kind], m.Covered, m.Total, Pct(m.Percent, nil), verdict)
}

// MainBlock is the main-number lines of a workflow: the number, then why.
func MainBlock(w coverage.WorkflowResult) []string {
	lines := []string{MainLine(w)}
	if w.Main.Measured && w.Main.Why != "" {
		lines = append(lines, "  Why this number: "+w.Main.Why+".")
	}
	if s := ScenarioLine(w); s != "" {
		lines = append(lines, s)
	}
	if s := SplitAdvice(w, nil); s != "" {
		lines = append(lines, s)
	}
	return lines
}

// ScenarioLine is "Scenarios: 12 (5 covered · ...)" for a workflow.
func ScenarioLine(w coverage.WorkflowResult) string {
	s := w.Scenarios
	if !s.Measured {
		if s.Why == "" {
			return ""
		}
		return "Scenarios: not measured (" + s.Why + ")"
	}
	parts := []string{fmt.Sprintf("%d covered", s.Covered)}
	if s.NeedsReview > 0 {
		parts = append(parts, fmt.Sprintf("%d needs-review", s.NeedsReview))
	}
	if s.NeedsSpecial > 0 {
		parts = append(parts, fmt.Sprintf("%d need a special test", s.NeedsSpecial))
	}
	if s.CannotBeTested > 0 {
		parts = append(parts, fmt.Sprintf("%d cannot be tested", s.CannotBeTested))
	}
	if !s.FromPaths && s.Unchecked != nil && s.Unchecked.Sign() > 0 {
		parts = append(parts, Big(s.Unchecked)+" not checked one by one, counted as testable")
	}
	return fmt.Sprintf("Scenarios: %s (%s) — different things the workflow can do, counted by what it does outside itself", Big(s.Count), strings.Join(parts, " · "))
}

// SameAsNumber is the number of the main path of the scenario of p when p is
// not the main path itself, else 0.
func SameAsNumber(p coverage.PathResult, all []coverage.PathResult) int {
	if p.SameAs == "" {
		return 0
	}
	for _, q := range all {
		if q.Path.ID() == p.SameAs {
			return q.Number
		}
	}
	return 0
}

// SameAs is the tag after a path that does what another path of its scenario
// does ("" for a main path).
func SameAs(p coverage.PathResult, all []coverage.PathResult) string {
	if n := SameAsNumber(p, all); n > 0 {
		return fmt.Sprintf(" (same behaviour as path %d)", n)
	}
	return ""
}

// SplitAdvice is one line for a workflow whose main number is not path
// coverage: the stage with the most paths, as a candidate for a child workflow
// ("" when there is no advice to give). It is advice only; no number changes.
func SplitAdvice(w coverage.WorkflowResult, display func(string) string) string {
	if w.Main.Kind == coverage.MainPath || len(w.Stages) < 2 || w.Exact == nil {
		return ""
	}
	best := -1
	for i, s := range w.Stages {
		if best < 0 || s.Count.Cmp(w.Stages[best].Count) > 0 {
			best = i
		}
	}
	s := w.Stages[best]
	if s.Count.Cmp(big.NewInt(1)) <= 0 || s.File == "" {
		return ""
	}
	file := s.File
	if display != nil {
		file = display(file)
	}
	held := fmt.Sprintf("%s scenarios", fmt.Sprint(s.Scenarios))
	if s.Scenarios == 0 {
		held = "too many to list its scenarios"
	}
	return fmt.Sprintf("Split advice: stage %d (%s:%d-%d) holds %s of the workflow's %s paths and %s; moving it into a child workflow would let it be tested on its own",
		s.Index+1, strings.ReplaceAll(file, "\\", "/"), s.FirstLine, s.LastLine, Big(s.Count), Big(w.Exact), held)
}

// TestsLine is one line about the Go tests the traces name: how many there are
// and which of them add nothing the others do not cover. "" when no trace was
// counted.
func TestsLine(res coverage.Result) string {
	named, nameless := 0, 0
	var useless []string
	for _, t := range res.Tests {
		if t.Name == coverage.NoTestName {
			nameless += t.Traces
			continue
		}
		named++
		if t.AddsNothing {
			useless = append(useless, t.Name)
		}
	}
	switch {
	case named == 0 && nameless == 0:
		return ""
	case named == 0:
		return "Tests: test names not recorded in these traces (run pathkit test again to record them)"
	}
	line := fmt.Sprintf("Tests: %d recorded", named)
	if len(useless) == 0 {
		return line + "; each adds a scenario or branch the others do not cover"
	}
	if len(useless) > 5 {
		return line + fmt.Sprintf("; %d add nothing the others do not cover (first: %s)", len(useless), strings.Join(useless[:5], ", "))
	}
	return line + fmt.Sprintf("; %d add%s nothing the others do not cover: %s", len(useless), map[bool]string{true: "s", false: ""}[len(useless) == 1], strings.Join(useless, ", "))
}

// TopTests is how many tests the "write first" list holds.
const TopTests = 10

// TestsToWriteFirstBlock prints the list of tests worth writing first, the
// riskiest first ("" when there is nothing to write).
func TestsToWriteFirstBlock(items []advice.Item) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Start here. Tests to write first (a scenario is one distinct way the workflow behaves from outside; missed means no test ran it). Riskiest first:\n")
	for i, it := range items {
		where := it.File
		if it.Line > 0 {
			where = fmt.Sprintf("%s:%d", it.File, it.Line)
		}
		fmt.Fprintf(&b, "  %d. [%s] %s — %s\n", i+1, it.Tag, it.Workflow, where)
		fmt.Fprintf(&b, "     %s\n", it.Hint)
		fmt.Fprintf(&b, "     suggested test name: %s · covers %s · to write it: pathkit scaffold %s\n", it.TestName, it.Covers(), it.ID)
	}
	return b.String()
}
