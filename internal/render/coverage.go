package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Pct prints a percentage with one decimal, like "52.6%". When a
// --fail-under threshold is given and one decimal would make the value
// look equal to it (52.59 vs 52.6), more decimals are printed until the
// difference shows ("52.59%"), so a pass or fail never looks wrong.
func Pct(value float64, threshold *float64) string {
	d := 1
	if threshold != nil && value != *threshold {
		for d < 6 && strconv.FormatFloat(value, 'f', d, 64) == strconv.FormatFloat(*threshold, 'f', d, 64) {
			d++
		}
	}
	return strconv.FormatFloat(value, 'f', d, 64) + "%"
}

// Threshold prints a --fail-under value as the user wrote it: "80%", "52.6%".
func Threshold(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

// CoverageText prints a coverage result in the setup guide's format. The
// total's percentage is threshold-aware (see Pct).
func CoverageText(res coverage.Result, threshold *float64) string {
	var b strings.Builder
	single := len(res.Workflows) == 1
	for i, w := range res.Workflows {
		if i > 0 {
			b.WriteString("\n")
		}
		name := w.Name
		if w.AddedByConfig {
			name += " (added by config)"
		}
		fmt.Fprintf(&b, "Workflow: %s\n", name)
		fmt.Fprintf(&b, "%s\n", TotalLine(model.PathList{List: make([]model.Path, len(w.Paths)), Truncated: w.Truncated}, model.DefaultMaxPaths))
		var th *float64
		if single {
			th = threshold // this workflow's number is the total
		}
		fmt.Fprintf(&b, "Covered: %d/%d (%s)\n", w.Covered, len(w.Paths), Pct(coverage.Percent(w.Covered, len(w.Paths)), th))
		fmt.Fprintf(&b, "Branches: %d/%d (%s)\n", w.BranchesTaken, w.Branches, Pct(coverage.Percent(w.BranchesTaken, w.Branches), nil))

		var covered, untested []string
		for _, p := range w.Paths {
			line := fmt.Sprintf("  %d. %s", p.Number, PathText(p.Path))
			if p.StaleTrace {
				line += " (stale trace)"
			}
			if p.Covered {
				covered = append(covered, line)
			} else {
				untested = append(untested, line)
			}
		}
		b.WriteString("\nCovered paths:\n")
		writeLines(&b, covered)
		b.WriteString("Untested paths:\n")
		writeLines(&b, untested)
	}
	if !single {
		fmt.Fprintf(&b, "\n%d paths total · %d covered · %d missed · %s coverage\n",
			res.Paths, res.Covered, res.Paths-res.Covered, Pct(coverage.Percent(res.Covered, res.Paths), threshold))
		fmt.Fprintf(&b, "Branches: %d/%d (%s)\n", res.BranchesTaken, res.Branches, Pct(coverage.Percent(res.BranchesTaken, res.Branches), nil))
	}
	return b.String()
}

func writeLines(b *strings.Builder, lines []string) {
	if len(lines) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
}

// TracesLine sums up every trace file read, so none disappears silently.
func TracesLine(c coverage.Counts) string {
	line := fmt.Sprintf("Traces: %d read · %d counted · %d unmatched · %d stale · %d incomplete · %d excluded · %d unknown workflow · %d unreadable",
		c.Read, c.Counted, c.Unmatched, c.Stale, c.Incomplete, c.Excluded, c.Unknown, c.Unreadable)
	if c.Other > 0 {
		line += fmt.Sprintf(" · %d for other workflows (--function)", c.Other)
	}
	return line + "\n"
}
