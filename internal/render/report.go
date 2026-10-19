package render

import (
	"fmt"
	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"math"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// ReportOptions controls report's text output.
type ReportOptions struct {
	Summary   bool     // one line per workflow instead of one block
	Color     bool     // colour the words covered/missed and the priority label
	Threshold *float64 // --fail-under, for the project total's decimals (see Pct)
	// ExcludedBy names what decided the scope (".pathkitrc.json",
	// "--include", "--exclude"), for the excluded line. Empty: no scope
	// in use.
	ExcludedBy []string
	// Classification, when not nil, adds the classification block (buckets,
	// testable and overall coverage, the basis used) before the excluded
	// line. nil prints exactly what PathKit printed before classification.
	Classification *ClassificationView
	// ExtraLines are printed after the classification block (the CI floor's
	// basis per workflow over the cap).
	ExtraLines []string
	// OmitCappedPaths leaves the path list out of the block of a workflow over
	// the cap (counts, stages and groups stay); --paths turns it off. The
	// zero value prints the lists as before.
	OmitCappedPaths bool
	// TopTests is the list of tests to write first (advice.TestsToWriteFirst).
	TopTests []advice.Item
	// Hints adds "how to cover it" under every testable path no test ran
	// (--hints).
	Hints bool
}

// ReportText prints a whole-project report: one block per workflow (or
// one line with Summary), the project total, the excluded workflows with
// their reasons, and the Traces line. The numbers are coverage.Compute's,
// exactly as coverage prints them.
func ReportText(res coverage.Result, opt ReportOptions) string {
	var b strings.Builder
	b.WriteString(CapWarning(res))
	b.WriteString(ZeroPathWarning(res))
	b.WriteString(MainHeadlineBlock(res)) // after the warnings, which stay first
	if opt.Summary {
		width := 0
		for _, w := range res.Workflows {
			width = max(width, len(reportName(w)))
		}
		for _, w := range res.Workflows {
			fmt.Fprintf(&b, "%-*s  %s · main number: %s\n", width, reportName(w), workflowStats(w, opt.Color, false), MainShort(w))
			if w.Main.Kind != coverage.MainPath {
				// the interesting ones get their reason, and the advice, on lines of their own
				if w.Main.Why != "" {
					b.WriteString("  Why this number: " + w.Main.Why + ".\n")
				}
				if a := SplitAdvice(w, nil); a != "" {
					b.WriteString("  " + a + "\n")
				}
			}
			for _, l := range HelperLines(w) {
				b.WriteString("  " + l + "\n")
			}
		}
	} else {
		for i, w := range res.Workflows {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s (%s)\n", reportName(w), w.File)
			for _, l := range MainBlock(w) {
				b.WriteString(l + "\n")
			}
			fmt.Fprintf(&b, "%s\n", workflowStats(w, opt.Color, true))
			b.WriteString(StageLines(w))
			for _, l := range HelperLines(w) {
				b.WriteString(l + "\n")
			}
			if w.Capped && opt.OmitCappedPaths {
				b.WriteString(CappedPathsNote(w))
				continue
			}
			for _, p := range w.Paths {
				word := paint("missed", red, opt.Color)
				if p.Covered {
					word = paint("covered", green, opt.Color)
				}
				line := fmt.Sprintf("  %d. %s: %s", p.Number, PathText(p.Path), word)
				if p.StaleTrace {
					line += " (stale trace)"
				}
				if opt.Classification != nil {
					line += ClassTag(p.Class)
				}
				line += SameAs(p, w.Paths)
				b.WriteString(line + "\n")
				if opt.Hints && WantsHint(p) {
					b.WriteString(hintLines(p))
				}
			}
		}
	}

	b.WriteString("\n")
	for _, l := range HeadlineLines(res, opt.Threshold, opt.Classification != nil) {
		b.WriteString(l + "\n")
	}
	if opt.Classification != nil {
		b.WriteString("\n" + ClassificationBlock(res, *opt.Classification))
	}
	for _, l := range opt.ExtraLines {
		b.WriteString(l + "\n")
	}
	if block := TestsToWriteFirstBlock(opt.TopTests); block != "" {
		b.WriteString("\n" + block)
	}
	b.WriteString("\n" + HandlerBlock(res, opt.Summary) + CallOutcomeBlock(res, opt.Summary) + LoopRoundBlock(res, opt.Summary))
	if l := TestsLine(res); l != "" {
		b.WriteString(l + "\n\n")
	}
	b.WriteString(UnreadBlock(res, UnreadTop))
	b.WriteString("\n" + ExcludedBlock(res.Excluded, opt.ExcludedBy))
	b.WriteString(TracesLine(res.Counts))
	return b.String()
}

// ProjectTotalLine is "38 paths total · 20 covered · 18 missed · 52.6%
// project coverage" (the percentage threshold-aware, see Pct). The text
// and HTML reports both print it.
func ProjectTotalLine(res coverage.Result, threshold *float64) string {
	pct := NotMeasured("no paths")
	if res.Paths > 0 {
		pct = Pct(coverage.Percent(res.Covered, res.Paths), threshold)
	}
	return fmt.Sprintf("%d paths total · %d covered · %d missed · %s project coverage",
		res.Paths, res.Covered, res.Paths-res.Covered, pct)
}

// BranchesLine is "Branches: 39/50 (78.0%)" for the whole project.
func BranchesLine(res coverage.Result) string {
	return "Branches: " + Ratio(res.BranchesTaken, res.Branches, nil, "no branches")
}

// WorkflowStats is a workflow's line in the full report, without colour:
// "2/3 paths · 66.7% · branches 3/4 · priority Medium", or with
// "(truncated at maxPaths=2000)" when the listing was cut. The text and
// HTML reports both print it.
func WorkflowStats(w coverage.WorkflowResult) string { return workflowStats(w, false, true) }

// ExcludedBlock is the excluded line followed by each excluded workflow
// with its reason. coverage and report both print it, always, even for 0
// (docs/DESIGN.md D8), so nobody can quietly raise coverage by excluding
// things; it is the one wording for both.
func ExcludedBlock(excluded []scope.Excluded, by []string) string {
	var b strings.Builder
	b.WriteString(ExcludedLine(len(excluded), by) + "\n")
	for _, e := range excluded {
		fmt.Fprintf(&b, "  %s: %s\n", e.Name, e.Reason)
	}
	return b.String()
}

// ExcludedLine is the first line of ExcludedBlock.
func ExcludedLine(n int, by []string) string {
	what := count(n, "workflow") + " excluded"
	switch {
	case len(by) == 0:
		return what + " (no scope in use)"
	case n == 0:
		return what + " by " + strings.Join(by, " and ")
	}
	return what + " by " + strings.Join(by, " and ") + ` (see "reason"):`
}

func count(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func reportName(w coverage.WorkflowResult) string {
	name := w.Name
	if w.AddedByConfig {
		name += " (added by config)"
	}
	if w.Module != "" {
		name += " [module " + w.Module + "]"
	}
	return name
}

// workflowStats is "2/3 paths · 66.7% · branches 3/4 · priority Medium".
func workflowStats(w coverage.WorkflowResult, color, branches bool) string {
	total := len(w.Paths)
	paths := fmt.Sprintf("%d/%d paths", w.Covered, total)
	if w.Truncated {
		paths = fmt.Sprintf("%d/%d+ paths (truncated at maxPaths=%d)", w.Covered, total, model.DefaultMaxPaths)
	}
	parts := []string{paths, WorkflowPct(w.Covered, total)}
	if w.Capped {
		parts = append(parts, "path coverage incomplete: "+Big(w.Exact)+" paths in all")
	}
	if len(w.Stages) > 1 || w.Capped {
		c, t, _ := StageCoverage(w)
		parts = append(parts, "stages "+Ratio(c, t, nil, "no stage paths to measure"))
	}
	if branches {
		parts = append(parts, fmt.Sprintf("branches %d/%d", w.BranchesTaken, w.Branches))
	}
	if label, ok := coverage.Priority(w.Covered, total); ok {
		parts = append(parts, "priority "+paint(label, priorityColor[label], color))
	} else {
		parts = append(parts, "no paths")
	}
	return strings.Join(parts, " · ")
}

// WorkflowPct prints a workflow's % so that it never looks like it sits on
// a D11 boundary it isn't on: 7999/10000 prints "79.99%" (Medium), not
// "80.0%", and 8001/10000 prints "80.01%" (Low).
func WorkflowPct(covered, total int) string {
	v := coverage.Percent(covered, total)
	boundary := 50.0
	if math.Abs(v-80) < math.Abs(v-50) {
		boundary = 80
	}
	return Pct(v, &boundary)
}

// Colours: ANSI codes, used only when the caller says so (a real
// terminal, and no --no-color, noColor or NO_COLOR).
const (
	red    = "31"
	green  = "32"
	yellow = "33"
)

var priorityColor = map[string]string{
	coverage.PriorityHigh:   red,
	coverage.PriorityMedium: yellow,
	coverage.PriorityLow:    green,
}

func paint(word, code string, on bool) string {
	if !on {
		return word
	}
	return "\x1b[" + code + "m" + word + "\x1b[0m"
}

// ColorEnabled decides whether report colours its text: only on a real
// terminal, and never with --no-color (or the config's noColor) or a
// non-empty NO_COLOR (https://no-color.org).
func ColorEnabled(noColor bool, noColorEnv string, terminal bool) bool {
	return terminal && !noColor && noColorEnv == ""
}
