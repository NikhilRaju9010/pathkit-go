package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// MaxGroupsText is how many groups the text report lists before it says how
// many more there are.
const MaxGroupsText = 25

// GroupView is one missed-path group in the words the reports print.
type GroupView struct {
	Workflow string
	Missed   int
	// Step is the first step no test took: "if x > 0 --false".
	Step string
	// File and Line are where the decision is in the code (empty and 0 when
	// the group differs only in how the workflow ends).
	File string
	Line int
	// After is the road the tests walked to the decision, "" at the start.
	After []string
	// NoTests: no test ran this workflow at all.
	NoTests bool
	// Hint is what a test must make happen.
	Hint string
}

// Where is "file:line", or "" when there is no place.
func (g GroupView) Where() string {
	if g.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", g.File, g.Line)
}

// Groups are the missed-path groups of a result, biggest first.
func Groups(res coverage.Result) []GroupView {
	var out []GroupView
	for _, g := range coverage.AllMissedGroups(res) {
		out = append(out, groupView(g))
	}
	return out
}

func groupView(g coverage.MissedGroup) GroupView {
	v := GroupView{Workflow: g.Workflow, Missed: g.Missed, NoTests: g.NoTests}
	for _, s := range g.Prefix {
		v.After = append(v.After, stepText(s))
	}
	if g.Step.Exit == nil {
		v.Step = "same decisions, but the workflow ends as " + strings.ToLower(g.End.String())
		if h := HowToCover(model.Path{End: g.End}); len(h) > 0 {
			v.Hint = h[len(h)-1]
		}
		return v
	}
	v.Step = stepText(g.Step)
	if p := g.Step.Junction.Pos; p.Filename != "" {
		v.File, v.Line = filepath.Base(p.Filename), p.Line
	}
	if h := HowToCover(model.Path{Steps: []model.Step{g.Step}}); len(h) > 0 {
		v.Hint = h[0]
	}
	return v
}

func stepText(s model.Step) string {
	if s.Exit == s.Junction.Retry {
		return "retry " + s.Junction.Label + s.Junction.Where()
	}
	return fmt.Sprintf("%s%s --%s", s.Junction.Label, s.Junction.Where(), s.Exit.Label)
}

// AfterText is the road the tests walked to a group's decision, shortened to
// its last three steps (the JSON has all of them).
func AfterText(after []string) string {
	if len(after) > 3 {
		return fmt.Sprintf("... (%d earlier steps) -> %s", len(after)-3, strings.Join(after[len(after)-3:], " -> "))
	}
	return strings.Join(after, " -> ")
}

// GroupsText is the "Missed paths by first untested step" section ("" when
// no testable path is missed).
func GroupsText(res coverage.Result) string {
	gs := Groups(res)
	capped, other := false, 0
	for _, w := range res.Workflows {
		capped = capped || w.Capped || w.Truncated
		for _, p := range w.Paths {
			if !p.Covered && p.Class.Bucket != classify.Testable {
				other++
			}
		}
	}
	if len(gs) == 0 {
		return ""
	}
	total := 0
	for _, g := range gs {
		total += g.Missed
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Missed paths by first untested step: %s in %s (the biggest %d are listed)\n", count(total, "testable path"), count(len(gs), "group"), min(len(gs), MaxGroupsText))
	b.WriteString("  (a group is the paths that leave the road the tests walked at the same step; biggest first.)\n")
	if capped {
		b.WriteString("  (A workflow with more paths than the list holds is grouped by its listed paths only.)\n")
	}
	if other > 0 {
		fmt.Fprintf(&b, "  (%s needs review and %s not grouped: only testable paths are.)\n", count(other, "more missed path"), map[bool]string{true: "is", false: "are"}[other == 1])
	}
	for i, g := range gs {
		if i == MaxGroupsText {
			fmt.Fprintf(&b, "  ... and %s more not listed here (report --groups --json lists them all)\n", count(len(gs)-i, "group"))
			break
		}
		fmt.Fprintf(&b, "\n  %d. %s: %s, first untested step: %s", i+1, g.Workflow, count(g.Missed, "path"), g.Step)
		if w := g.Where(); w != "" {
			fmt.Fprintf(&b, "  (%s)", w)
		}
		b.WriteString("\n")
		if g.NoTests {
			b.WriteString("       no test ran this workflow\n")
		} else if len(g.After) == 0 {
			b.WriteString("       the tests get this far: only the start of the workflow\n")
		} else {
			fmt.Fprintf(&b, "       the tests get this far: %s\n", AfterText(g.After))
		}
		if g.Hint != "" {
			fmt.Fprintf(&b, "       how to cover it: %s\n", g.Hint)
		}
	}
	return b.String()
}
