package render

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Big prints a big whole number with commas: 3,355,443,200.
func Big(n *big.Int) string {
	if n == nil {
		return "0"
	}
	s := n.String()
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	r := strings.Join(out, ",")
	if neg {
		r = "-" + r
	}
	return r
}

// ShowStages says whether a workflow's stages are printed: it has more paths
// than the list holds, or parts of its map were left out of the list as
// impossible. A workflow whose paths are all listed prints as before: its
// path numbers use every rule and override, which the stage numbers do not.
func ShowStages(w coverage.WorkflowResult) bool {
	return w.Capped || len(w.Pruned) > 0
}

// StageCoverage counts, over the stages that are small enough to list, the
// stage paths a test ran and the ones a test can run (those not shown
// impossible).
func StageCoverage(w coverage.WorkflowResult) (covered, total int, bigStages int) {
	for _, s := range w.Stages {
		if s.Big {
			bigStages++
			continue
		}
		covered += s.Covered
		total += len(s.Paths) - s.CannotBeTested
	}
	return
}

// StageLines are the lines about stages and exact counts that follow a
// workflow's totals ("" when the workflow is small and has a single stage):
// the exact number of paths, what the list holds, the parts left out as
// impossible, stage coverage, and one line per stage.
func StageLines(w coverage.WorkflowResult) string {
	if !ShowStages(w) {
		return ""
	}
	var b strings.Builder
	if w.Capped {
		fmt.Fprintf(&b, "Exact paths: %s (%d listed; path coverage is incomplete)\n", Big(w.Exact), len(w.Paths))
	} else {
		fmt.Fprintf(&b, "Exact paths: %s\n", Big(w.Exact))
	}
	if n := len(w.Pruned); n > 0 {
		total, kept := new(big.Int), 0
		for _, p := range w.Pruned {
			if p.Unpruned {
				kept++
				continue
			}
			total.Add(total, p.Count)
		}
		fmt.Fprintf(&b, "Left out of the list as impossible (in the part of the map the list reached): %s paths in %s", Big(total), count(n-kept, "group"))
		if kept > 0 {
			fmt.Fprintf(&b, " (%d more groups under review: a test ran a path inside them)", kept)
		}
		b.WriteString("\n")
	}
	if len(w.Stages) > 0 {
		c, t, big := StageCoverage(w)
		line := fmt.Sprintf("Stage coverage: %d/%d (%s) in %d stages", c, t, Pct(coverage.Percent(c, t), nil), len(w.Stages))
		if big > 0 {
			line += fmt.Sprintf("; %d too big to list (branch coverage only)", big)
		}
		b.WriteString(line + "\n")
		for _, s := range w.Stages {
			b.WriteString("  " + StageLine(s) + "\n")
		}
	}
	return b.String()
}

// StageLine describes one stage on one line.
func StageLine(s coverage.StageResult) string {
	if s.Big {
		return fmt.Sprintf("Stage %d: %s paths, too many to list; branches %d/%d", s.Index+1, Big(s.Count), s.BranchesTaken, s.Branches)
	}
	line := fmt.Sprintf("Stage %d: %s paths, %d covered (%s); branches %d/%d", s.Index+1, Big(s.Count), s.Covered,
		Pct(coverage.Percent(s.Covered, len(s.Paths)-s.CannotBeTested), nil), s.BranchesTaken, s.Branches)
	if s.CannotBeTested > 0 {
		line += fmt.Sprintf("; %d cannot be tested", s.CannotBeTested)
	}
	if s.NeedsReview > 0 {
		line += fmt.Sprintf("; %d need review", s.NeedsReview)
	}
	if s.CutOff {
		line += "; more incoming states than the limit were cut off"
	}
	return line
}

// StagePathText prints one stage path like PathText: the steps, then how the
// workflow ends there, or "-> next stage" when it goes on.
func StagePathText(p model.StagePath) string {
	var b strings.Builder
	b.WriteString("Stage start ->")
	for _, s := range p.Steps {
		if s.Exit == s.Junction.Retry {
			b.WriteString(" retry -->")
			continue
		}
		fmt.Fprintf(&b, " %s%s --%s-->", s.Junction.Label, s.Junction.Where(), s.Exit.Label)
	}
	if p.Continue {
		b.WriteString(" next stage")
	} else {
		b.WriteString(" " + p.End.String())
	}
	return b.String()
}

// ProjectStageLine is the project's stage coverage over the workflows that
// have several stages or more paths than the list holds, and how many paths
// those workflows have in all ("" when there is no such workflow).
func ProjectStageLine(res coverage.Result) string {
	n, covered, total, bigStages := 0, 0, 0, 0
	exact := new(big.Int)
	capped := 0
	for _, w := range res.Workflows {
		if !ShowStages(w) {
			continue
		}
		n++
		c, t, b := StageCoverage(w)
		covered, total, bigStages = covered+c, total+t, bigStages+b
		exact.Add(exact, w.Exact)
		if w.Capped {
			capped++
		}
	}
	if n == 0 {
		return ""
	}
	line := fmt.Sprintf("Stage coverage: %d/%d (%s) over %s with stages (a stage is a stretch of the workflow between two decisions every path goes through; the paths of the stages multiply into the workflow's paths)", covered, total, Pct(coverage.Percent(covered, total), nil), count(n, "workflow"))
	if bigStages > 0 {
		line += fmt.Sprintf("; %s too big to list left out (branch coverage only)", count(bigStages, "stage"))
	}
	if capped > 0 {
		line += fmt.Sprintf("; path coverage is incomplete for %s (%s paths in all)", count(capped, "workflow"), Big(exact))
	}
	return line
}

// HeadlineLines are the project's headline lines. With no workflow over the
// cap they are the plain totals (as always); with one, the three numbers of
// coverage.Headline: path coverage over the workflows under the cap, stage
// coverage over the ones over it, and branch coverage over all. classified
// says whether classification is on (the numbers are then the testable ones).
func HeadlineLines(res coverage.Result, threshold *float64, classified bool) []string {
	h, ok := res.Headline()
	if !ok {
		lines := []string{ProjectTotalLine(res, threshold), BranchesLine(res)}
		if l := ProjectStageLine(res); l != "" {
			lines = append(lines, l)
		}
		return lines
	}
	name := func(s string) string {
		if classified {
			return s + " (testable)"
		}
		return s
	}
	path := fmt.Sprintf("%s: %d/%d (%s) over %s; %s too big for path coverage, see stage coverage",
		name("Path coverage"), h.PathCovered, h.PathTotal, Pct(coverage.Percent(h.PathCovered, h.PathTotal), threshold), count(h.PathWorkflows, "workflow"), count(h.TooBig, "workflow"))
	stage := fmt.Sprintf("%s: %d/%d (%s) over the %s too big for path coverage (a stage is a stretch of the workflow between two decisions every path goes through)",
		name("Stage coverage"), h.StageCovered, h.StageTotal, Pct(coverage.Percent(h.StageCovered, h.StageTotal), nil), count(h.TooBig, "workflow"))
	if h.StagesTooBig > 0 {
		stage += fmt.Sprintf("; %s too big to list left out (branch coverage only)", count(h.StagesTooBig, "stage"))
	}
	branch := fmt.Sprintf("Branch coverage: %d/%d (%s) over all workflows, the whole map of each", h.BranchCovered, h.BranchTotal, Pct(coverage.Percent(h.BranchCovered, h.BranchTotal), nil))
	return []string{path, stage, branch}
}
