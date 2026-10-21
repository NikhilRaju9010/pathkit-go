package render

import "testing"

func TestPlainStageWholeWordsOnly(t *testing.T) {
	for in, want := range map[string]string{
		"3 stages, 1 stage":                      "3 sections of the workflow, 1 section of the workflow",
		"stage one\nstage two":                   "section of the workflow one\nsection of the workflow two",
		"the staging.Workflow":                   "the staging.Workflow",
		"in workflows/stage/run.go":              "in workflows/stage/run.go",
		"`stage` is code":                        "`stage` is code",
		"stage_two and staged":                   "stage_two and staged",
		"Branch coverage (all workflows): 24/37": "Exits taken, older measure (all workflows): 24/37",
		"5/40 paths · 12.5% · branches 21/32":    "5/40 paths · 12.5% · exits taken 21/32",
		"needs-review (stage 1 of 2)":            "needs a person to check (section of the workflow 1 of 2)",
	} {
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
}
