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

// NotMeasured is how a ratio with nothing to divide by reads: "not measured
// (0 workflows under the cap)". It never reads as 0/0 or 0.0%.
func NotMeasured(reason string) string { return "not measured (" + reason + ")" }

// Ratio is "1/4 (25.0%)", or NotMeasured(reason) when total is 0.
func Ratio(covered, total int, threshold *float64, reason string) string {
	if total == 0 {
		return NotMeasured(reason)
	}
	return fmt.Sprintf("%d/%d (%s)", covered, total, Pct(coverage.Percent(covered, total), threshold))
}

// Threshold prints a --fail-under value as the user wrote it: "80%", "52.6%".
func Threshold(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

// CoverageText prints a coverage result in the setup guide's format. The
// total's percentage is threshold-aware (see Pct).
func CoverageText(res coverage.Result, threshold *float64) string {
	return CoverageTextTagged(res, threshold, false)
}

// CoverageTextTagged is CoverageText with, after each path that something
// written down classified (a rule, an override, a comment), a tag naming
// the bucket, label, source and rule id (ClassTag), when tags is true.
func CoverageTextTagged(res coverage.Result, threshold *float64, tags bool) string {
	return CoverageTextHints(res, threshold, tags, false)
}

// CoverageTextHints is CoverageTextTagged with, when hints is true, "how
// to cover it" under every testable untested path (--hints).
func CoverageTextHints(res coverage.Result, threshold *float64, tags, hints bool) string {
	return CoverageTextFull(res, threshold, tags, hints, false)
}

// CappedPathsNote is the line that stands where a capped workflow's path list would be.
func CappedPathsNote(w coverage.WorkflowResult) string {
	return fmt.Sprintf("Paths: not listed (the first %d of %s are in the list; --paths prints them)\n", len(w.Paths), Big(w.Exact))
}

// CoverageTextFull is CoverageTextHints that, when omitCapped is true, leaves
// the covered and untested lists out of a workflow over the cap (--paths
// brings them back).
func CoverageTextFull(res coverage.Result, threshold *float64, tags, hints, omitCapped bool) string {
	var b strings.Builder
	b.WriteString(CapWarning(res))
	b.WriteString(ZeroPathWarning(res))
	b.WriteString(MainHeadlineBlock(res)) // after the warnings, which stay first
	single := len(res.Workflows) == 1
	for i, w := range res.Workflows {
		if i > 0 {
			b.WriteString("\n")
		}
		name := w.Name
		if w.AddedByConfig {
			name += " (added by config)"
		}
		if w.Module != "" {
			name += " [module " + w.Module + "]"
		}
		fmt.Fprintf(&b, "Workflow: %s\n", name)
		for _, l := range MainBlock(w) {
			b.WriteString(l + "\n")
		}
		fmt.Fprintf(&b, "%s\n", TotalLine(model.PathList{List: make([]model.Path, len(w.Paths)), Truncated: w.Truncated}, model.DefaultMaxPaths))
		var th *float64
		if single {
			th = threshold // this workflow's number is the total
		}
		fmt.Fprintf(&b, "Covered: %d/%d (%s)\n", w.Covered, len(w.Paths), Pct(coverage.Percent(w.Covered, len(w.Paths)), th))
		fmt.Fprintf(&b, "Branches: %d/%d (%s)\n", w.BranchesTaken, w.Branches, Pct(coverage.Percent(w.BranchesTaken, w.Branches), nil))
		b.WriteString(StageLines(w))
		for _, l := range HelperLines(w) {
			b.WriteString(l + "\n")
		}
		if w.Capped && omitCapped {
			b.WriteString(CappedPathsNote(w))
			continue
		}

		var covered, untested []string
		for _, p := range w.Paths {
			line := fmt.Sprintf("  %d. %s", p.Number, PathText(p.Path))
			if p.StaleTrace {
				line += " (stale trace)"
			}
			if tags {
				line += ClassTag(p.Class)
			}
			line += SameAs(p, w.Paths)
			if p.Covered {
				covered = append(covered, line)
			} else {
				if hints && WantsHint(p) {
					line += "\n" + strings.TrimSuffix(hintLines(p), "\n")
				}
				untested = append(untested, line)
			}
		}
		b.WriteString("\nCovered paths:\n")
		writeLines(&b, covered)
		b.WriteString("Untested paths:\n")
		writeLines(&b, untested)
	}
	if !single {
		if _, capped := res.Headline(); capped {
			b.WriteString("\n")
			for _, l := range HeadlineLines(res, threshold, tags) {
				b.WriteString(l + "\n")
			}
		} else {
			fmt.Fprintf(&b, "\n%d paths total · %d covered · %d missed · %s coverage\n",
				res.Paths, res.Covered, res.Paths-res.Covered, Pct(coverage.Percent(res.Covered, res.Paths), threshold))
			fmt.Fprintf(&b, "Branches: %d/%d (%s)\n", res.BranchesTaken, res.Branches, Pct(coverage.Percent(res.BranchesTaken, res.Branches), nil))
			if l := ProjectStageLine(res); l != "" {
				b.WriteString(l + "\n")
			}
		}
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
