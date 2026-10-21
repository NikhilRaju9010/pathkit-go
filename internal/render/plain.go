package render

import (
	"regexp"
	"strings"
)

// plainWords turns the technical words of the older report blocks into the
// plain names (milestone M11, R1). The longest phrases come first. A word that
// is a path or a file name inside backticks is not touched because none of the
// phrases below appear in code.
var plainWords = strings.NewReplacer(
	"cannot-be-tested", "can't happen",
	"Cannot be tested", "Can't happen",
	"needs-special-test", "needs a special test",
	"Needs special test", "Needs a special test",
	"needs-review", "needs a person to check",
	"Needs review", "Needs a person to check",
	"known-defect", "blocked by a known bug",
	"likely-defect", "possible bug",
	"unmatched traces", "test runs PathKit couldn't place",
	"Decision pairs", "Two-step combinations",
	"decision pairs", "two-step combinations",
	// the older exit-based figures keep their numbers; their names must not
	// clash with the new "Branch coverage" headline
	"Branch coverage (all workflows):", "Exits taken, older measure (all workflows):",
	" · branches ", " · exits taken ",
	"Traces:", "Test runs:",
	" traces ", " test runs ",
	"trace files", "test-run files",
)

// stageWord finds "stage" or "stages" as a whole word: not part of a longer
// word, a file name or a path ("staging", "stage/x.go", "stage_two").
var stageWord = regexp.MustCompile(`(^|[^\w/.\-])(stages?)([^\w/.\-]|$)`)

// Plain rewrites a block of the technical report in plain names. Text between
// backticks (code, names) is left as it is.
func Plain(text string) string {
	parts := strings.Split(text, "`")
	for i := 0; i < len(parts); i += 2 {
		parts[i] = plainWords.Replace(parts[i])
		// twice: two matches can share the character between them
		for range 2 {
			parts[i] = stageWord.ReplaceAllStringFunc(parts[i], plainStage)
		}
	}
	return strings.Join(parts, "`")
}

func plainStage(m string) string {
	g := stageWord.FindStringSubmatch(m)
	if g[2] == "stages" {
		return g[1] + "sections of the workflow" + g[3]
	}
	return g[1] + "section of the workflow" + g[3]
}
