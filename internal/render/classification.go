package render

import (
	"fmt"
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
	lines := []string{"Classification:", fmt.Sprintf("  Total paths:         %d", b.Overall().Paths), fmt.Sprintf("  Testable:            %d", b.Testable.Paths)}
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
		lines = append(lines, fmt.Sprintf("  %s %d", bk.name, bk.count))
		lines = append(lines, reasons[bk.bucket]...)
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

// ClassificationBlock is ClassificationLines as text ending in a newline.
func ClassificationBlock(res coverage.Result, v ClassificationView) string {
	return strings.Join(ClassificationLines(res, v), "\n") + "\n"
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
