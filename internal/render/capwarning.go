package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// CapNames returns the workflows whose path list was cut at the path cap
// (model.DefaultMaxPaths), in report order. Empty when none was.
func CapNames(res coverage.Result) []string {
	var names []string
	for _, w := range res.Workflows {
		if w.Truncated {
			names = append(names, w.Name)
		}
	}
	return names
}

// CapHeadline is the one-line warning that names the workflows.
func CapHeadline(names []string) string {
	what := "1 workflow has"
	if len(names) != 1 {
		what = fmt.Sprintf("%d workflows have", len(names))
	}
	return fmt.Sprintf("WARNING: %s more than %d paths, so its path list and classification are incomplete: %s",
		what, model.DefaultMaxPaths, strings.Join(names, ", "))
}

// CapAdvice says what the cut means and what to do. The text, HTML and JSON
// reports all print these lines, so they never disagree.
func CapAdvice() []string {
	return []string{
		fmt.Sprintf("PathKit lists only the first %d paths of such a workflow. Paths beyond that are not listed, not classified and not counted, so the numbers for it are a lower bound.", model.DefaultMaxPaths),
		"A test that ran a path beyond the cap shows up as an unmatched trace; that does not mean your code or a config entry is out of date.",
		"What to do: split the workflow into smaller ones (for example child workflows), or move some decisions into activities, so each workflow stays under the cap. Until then, read this workflow's numbers as incomplete.",
	}
}

// CapWarning is the loud block at the top of the text reports: "" when no
// workflow reached the cap, so existing output is unchanged.
func CapWarning(res coverage.Result) string {
	names := CapNames(res)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(CapHeadline(names) + "\n")
	for _, l := range CapAdvice() {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\n")
	return b.String()
}
