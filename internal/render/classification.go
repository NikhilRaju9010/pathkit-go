package render

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// ClassificationView says how the classification block is printed.
type ClassificationView struct {
	// Basis is the one --fail-under uses ("testable" or "overall"). It is
	// always printed.
	Basis coverage.Basis
	// Threshold is --fail-under, for Pct's extra decimals; nil for none.
	Threshold *float64
	// Notes are lines about where the markings came from (the engine's
	// Notes), printed under the bucket counts. Empty when nothing was
	// configured.
	Notes []string
	// Panel is the assumptions the claims rest on, on or off, with how many
	// claims rest on each (classify.Engine.AssumptionPanel). Empty: not printed.
	Panel []classify.AssumptionStatus
}

// ClassificationLines is the classification block, one string per line, in
// the order of docs/DESIGN.md D13. The text report and
// the HTML report both print these exact lines.
func ClassificationLines(res coverage.Result, v ClassificationView) []string {
	b := res.Breakdown
	var th *float64
	if v.Basis == coverage.BasisTestable {
		th = v.Threshold
	}
	var thOverall *float64
	if v.Basis == coverage.BasisOverall {
		thOverall = v.Threshold
	}
	line := func(name string, c coverage.BucketCount, th *float64, note string) string {
		s := fmt.Sprintf("%-22s %d / %d", name+":", c.Covered, c.Paths)
		if c.Paths > 0 {
			s += " = " + Pct(coverage.Percent(c.Covered, c.Paths), th)
		}
		return s + note
	}
	special := b.Special()
	capped, left, leftLines := leftOutImpossible(res)
	head := "Classification:"
	if capped > 0 {
		head = fmt.Sprintf("Classification (listed paths only; %s over the cap):", count(capped, "workflow"))
	}
	lines := []string{head, fmt.Sprintf("  Total paths:         %d", b.Overall().Paths), fmt.Sprintf("  Testable:            %d", b.Testable.Paths)}
	reasons := reasonLines(res)
	for _, bk := range []struct {
		name   string
		bucket classify.Bucket
		count  int
	}{
		{"Cannot be tested:   ", classify.CannotBeTested, b.CannotBeTested.Paths},
		{"Needs special test: ", classify.NeedsSpecialTest, b.NeedsSpecialTest.Paths},
		{"Needs review:       ", classify.NeedsReview, b.NeedsReview.Paths},
	} {
		text := fmt.Sprintf("  %s %d", bk.name, bk.count)
		if bk.bucket == classify.CannotBeTested && left.Sign() > 0 {
			text += fmt.Sprintf(" listed, plus %s left out of the list as impossible (below)", Big(left))
		}
		lines = append(lines, text)
		lines = append(lines, reasons[bk.bucket]...)
	}
	lines = append(lines, leftLines...)
	if capped > 0 {
		if tc := b.TestableCoverage(); tc.Paths != b.Testable.Paths {
			policy := string(b.Policy)
			if policy == "" {
				policy = "count_as_testable"
			}
			if b.Policy == classify.Exclude {
				lines = append(lines, fmt.Sprintf("  Testable %d + unexplained %d = %d in the denominator (policy: %s)", b.Testable.Paths, b.Unexplained.Paths, tc.Paths, policy))
			} else {
				lines = append(lines, fmt.Sprintf("  Testable %d + needs-review %d = %d in the denominator (policy: %s)", b.Testable.Paths, b.NeedsReview.Paths, tc.Paths, policy))
			}
		}
	}
	if n := BlockedByKnownBugs(res); n > 0 {
		lines = append(lines, fmt.Sprintf("  Blocked by known bugs: %d", n))
	}
	for _, n := range v.Notes {
		lines = append(lines, "  "+n)
	}
	lines = append(lines,
		line("Testable coverage", b.TestableCoverage(), th, "  (testable paths only)"),
		line("Special-test coverage", special, nil, ""),
		line("Overall coverage", b.Overall(), thOverall, "  (all paths)"))
	use := fmt.Sprintf("%-22s %s", "Coverage basis used:", v.Basis)
	if v.Threshold != nil {
		use += " (for --fail-under)"
	}
	return append(lines, use)
}

// LeftOut is what the workflows' lists leave out as impossible.
type LeftOut struct {
	// Capped is how many workflows are over the cap.
	Capped int
	// Paths and Groups: the paths left out of the lists as impossible (pruned
	// groups no trace contradicted), and in how many groups.
	Paths  *big.Int
	Groups int
	// ByLabel is Paths split by label.
	ByLabel map[string]*big.Int
}

// LeftOutImpossible collects the pruned groups of every workflow.
func LeftOutImpossible(res coverage.Result) LeftOut {
	lo := LeftOut{Paths: new(big.Int), ByLabel: map[string]*big.Int{}}
	for _, w := range res.Workflows {
		if w.Capped {
			lo.Capped++
		}
		for _, p := range w.Pruned {
			if p.Unpruned {
				continue
			}
			lo.Groups++
			lo.Paths.Add(lo.Paths, p.Count)
			label := p.Class.Label
			if label == "" {
				label = "(no label)"
			}
			if lo.ByLabel[label] == nil {
				lo.ByLabel[label] = new(big.Int)
			}
			lo.ByLabel[label].Add(lo.ByLabel[label], p.Count)
		}
	}
	return lo
}

// leftOutImpossible is LeftOutImpossible as the workflow count, the total and
// the lines of the classification block (none when nothing was left out).
func leftOutImpossible(res coverage.Result) (capped int, total *big.Int, lines []string) {
	lo := LeftOutImpossible(res)
	if lo.Groups == 0 {
		return lo.Capped, lo.Paths, nil
	}
	lines = append(lines, fmt.Sprintf("  Left out of the lists as impossible: %s paths in %s (not in the numbers above)", Big(lo.Paths), count(lo.Groups, "group")))
	var labels []string
	for l := range lo.ByLabel {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, l := range labels {
		lines = append(lines, fmt.Sprintf("    %s: %s", l, Big(lo.ByLabel[l])))
	}
	return lo.Capped, lo.Paths, lines
}

// ClassificationBlock is ClassificationLines as text ending in a newline.
func ClassificationBlock(res coverage.Result, v ClassificationView) string {
	return strings.Join(append(ClassificationLines(res, v), AssumptionPanelLines(v.Panel)...), "\n") + "\n"
}

// AssumptionPanelLines are the lines of the assumptions panel, which every
// report shows when classification is on: each assumption, on or off, and
// how many claims rest on it.
func AssumptionPanelLines(panel []classify.AssumptionStatus) []string {
	if len(panel) == 0 {
		return nil
	}
	lines := []string{"Assumptions the claims rest on (a claim is a path PathKit calls impossible; \"on\" means PathKit uses the assumption):"}
	for _, a := range panel {
		state := "on"
		if !a.On {
			state = "OFF: " + a.Why
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s; %s", a.Name+":", state, ClaimsText(a.Claims)))
	}
	return lines
}

// ClaimsText is "no claim rests on it", "1 claim rests on it" or "N claims
// rest on it".
func ClaimsText(n int) string {
	switch n {
	case 0:
		return "no claim rests on it"
	case 1:
		return "1 claim rests on it"
	}
	return fmt.Sprintf("%d claims rest on it", n)
}

// BlockedByKnownBugs counts the paths a person marked known-defect and no
// test has run (a path a test ran is testable and not counted).
func BlockedByKnownBugs(res coverage.Result) int {
	n := 0
	for _, w := range res.Workflows {
		for _, p := range w.Paths {
			if p.Class.Label == classify.KnownDefect && p.Class.Bucket == classify.NeedsReview {
				n++
			}
		}
	}
	return n
}

// reasonLines is, for each bucket other than testable, one line per reason
// label: how many paths, and how many were decided automatically and how
// many by a person ("    manual: 2 [auto 0 | manual 2]"). The counts per
// bucket add up to the bucket's total.
func reasonLines(res coverage.Result) map[classify.Bucket][]string {
	type n struct{ auto, manual int }
	counts := map[classify.Bucket]map[string]*n{}
	for _, w := range res.Workflows {
		for _, p := range w.Paths {
			bk := p.Class.Bucket
			if bk == classify.Testable {
				continue
			}
			if counts[bk] == nil {
				counts[bk] = map[string]*n{}
			}
			label := p.Class.Label
			if label == "" {
				label = "(no label)"
			}
			if counts[bk][label] == nil {
				counts[bk][label] = &n{}
			}
			if p.Class.Source == classify.SourceManual {
				counts[bk][label].manual++
			} else {
				counts[bk][label].auto++
			}
		}
	}
	out := map[classify.Bucket][]string{}
	for bk, m := range counts {
		var labels []string
		for l := range m {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		for _, l := range labels {
			c := m[l]
			out[bk] = append(out[bk], fmt.Sprintf("    %s: %d [auto %d | manual %d]", l, c.auto+c.manual, c.auto, c.manual))
		}
	}
	return out
}

// ClassTag is the note printed after a path that was classified by
// something written down: "[cannot-be-tested: loop-minimum · manual ·
// rule "polling-no-poll"]". A rule's id is always shown; so is a config
// override or a //pathkit:untestable comment. A detector's name is shown for paths a
// detector decided. A plain testable path has no tag ("").
func ClassTag(c classify.Classification) string {
	if c.Origin == "" && c.Bucket == classify.Testable {
		return ""
	}
	label := c.Label
	if label == "" {
		label = "no label"
	}
	parts := []string{label, string(c.Source)}
	if c.Detector != "" {
		parts = append(parts, "detector "+c.Detector)
	}
	switch c.Origin {
	case classify.OriginRule:
		parts = append(parts, fmt.Sprintf("rule %q", c.RuleID))
	case classify.OriginOverride:
		parts = append(parts, "config override")
	case classify.OriginComment:
		parts = append(parts, "//pathkit:untestable")
	}
	return fmt.Sprintf(" [%s: %s]", c.Bucket, strings.Join(parts, " · "))
}
