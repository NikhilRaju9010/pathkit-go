package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// ZeroPathNames returns the workflows that have no path at all, in report
// order. A workflow cut at the path cap is not one of them: it lists paths.
// A straight-line workflow has one path and is not one of them either.
func ZeroPathNames(res coverage.Result) []string {
	var names []string
	for _, w := range res.Workflows {
		if len(w.Paths) == 0 && !w.Truncated {
			names = append(names, w.Name)
		}
	}
	return names
}

// ZeroPathHeadline is the one-line warning that names the workflows.
func ZeroPathHeadline(names []string) string {
	what := "1 workflow, which has"
	if len(names) != 1 {
		what = fmt.Sprintf("%d workflows, which have", len(names))
	}
	return fmt.Sprintf("WARNING: PathKit found no decision points to measure in %s no paths: %s", what, strings.Join(names, ", "))
}

// ZeroPathAdvice says why it happens and what to do. The text report and
// stderr print the same lines.
func ZeroPathAdvice() []string {
	return []string{
		"A workflow gets no paths when PathKit finds no way for it to finish, for example a loop with no break and no return. Its 0 of 0 coverage is not a measurement, and it is not counted in the totals.",
		"What to do: if the workflow should end (or hand over with continue-as-new), give the loop a break or a return so that PathKit can see where it ends; if it is meant to run for ever, leave it out in .pathkitrc.json with a reason.",
	}
}

// ZeroPathWarning is the block in the text reports: "" when every workflow
// has at least one path, so existing output is unchanged.
func ZeroPathWarning(res coverage.Result) string {
	names := ZeroPathNames(res)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ZeroPathHeadline(names) + "\n")
	for _, l := range ZeroPathAdvice() {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\n")
	return b.String()
}
