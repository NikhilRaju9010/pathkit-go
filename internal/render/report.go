package render

import (
	"fmt"
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
}

// ReportText prints a whole-project report: one block per workflow (or
// one line with Summary), the project total, the excluded workflows with
// their reasons, and the Traces line. The numbers are coverage.Compute's,
// exactly as coverage prints them.
func ReportText(res coverage.Result, opt ReportOptions) string {
	var b strings.Builder
	if opt.Summary {
		width := 0
		for _, w := range res.Workflows {
			width = max(width, len(reportName(w)))
		}
		for _, w := range res.Workflows {
			fmt.Fprintf(&b, "%-*s  %s\n", width, reportName(w), workflowStats(w, opt.Color, false))
		}
	} else {
		for i, w := range res.Workflows {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s (%s)\n", reportName(w), w.File)
			fmt.Fprintf(&b, "%s\n", workflowStats(w, opt.Color, true))
			for _, p := range w.Paths {
				word := paint("missed", red, opt.Color)
				if p.Covered {
					word = paint("covered", green, opt.Color)
				}
				line := fmt.Sprintf("  %d. %s: %s", p.Number, PathText(p.Path), word)
				if p.StaleTrace {
					line += " (stale trace)"
				}
				b.WriteString(line + "\n")
			}
		}
	}

	fmt.Fprintf(&b, "\n%d paths total · %d covered · %d missed · %s project coverage\n",
		res.Paths, res.Covered, res.Paths-res.Covered, Pct(coverage.Percent(res.Covered, res.Paths), opt.Threshold))
	fmt.Fprintf(&b, "Branches: %d/%d (%s)\n", res.BranchesTaken, res.Branches, Pct(coverage.Percent(res.BranchesTaken, res.Branches), nil))
	b.WriteString("\n" + ExcludedBlock(res.Excluded, opt.ExcludedBy))
	b.WriteString(TracesLine(res.Counts))
	return b.String()
}

// ExcludedBlock is the excluded line followed by each excluded workflow
// with its reason. coverage and report both print it, always, even for 0
// (CLAUDE.md D8), so nobody can quietly raise coverage by excluding
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
	if w.AddedByConfig {
		return w.Name + " (added by config)"
	}
	return w.Name
}

// workflowStats is "2/3 paths · 66.7% · branches 3/4 · priority Medium".
func workflowStats(w coverage.WorkflowResult, color, branches bool) string {
	total := len(w.Paths)
	paths := fmt.Sprintf("%d/%d paths", w.Covered, total)
	if w.Truncated {
		paths = fmt.Sprintf("%d/%d+ paths (truncated at maxPaths=%d)", w.Covered, total, model.DefaultMaxPaths)
	}
	parts := []string{paths, WorkflowPct(w.Covered, total)}
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
