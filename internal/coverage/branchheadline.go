package coverage

import (
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// NoDecisions is the grade of a workflow with no decision outcome that can
// happen: branch coverage is always measured, and here it says so.
const NoDecisions = "no decisions to test"

// BranchHeadline is branch coverage over the whole run: the headline of every
// report (milestone M11, B4).
type BranchHeadline struct {
	BranchSummary
	Percent float64
	Grade   string
	// Pairs and condition parts are detail lines under the headline.
	PairsCovered, PairsTotal int
	Conditions               ConditionSummary
	// Workflows, AtTarget and TargetGrade: how many workflows reach the grade the
	// team aims for (branches.target_grade), not counting the ones with no
	// decisions to test.
	Workflows, AtTarget, NoDecision int
	TargetGrade                     string
}

// BranchHeadline works out the headline.
func (r Result) BranchHeadline() BranchHeadline {
	set := r.BranchSettings
	if set.TargetGrade == "" {
		set = scope.DefaultBranches()
	}
	h := BranchHeadline{BranchSummary: r.BranchCov, Percent: r.BranchCov.Percent(), Conditions: r.Conditions, TargetGrade: set.TargetGrade}
	h.Grade = set.Grades.Grade(h.Percent)
	if r.BranchCov.Possible == 0 {
		h.Grade = NoDecisions
	}
	floor := set.Grades.Floor(set.TargetGrade)
	for _, w := range r.Workflows {
		h.PairsCovered += w.Pairs.Covered
		h.PairsTotal += w.Pairs.Total
		h.Workflows++
		if w.BranchCov.Possible == 0 {
			h.NoDecision++
			continue
		}
		if w.BranchCov.Percent() >= floor {
			h.AtTarget++
		}
	}
	return h
}

// WorkflowBranchRow is one workflow's line in the table, worst first.
type WorkflowBranchRow struct {
	Name string
	BranchSummary
	Percent                  float64
	Grade                    string
	Missed                   int
	PairsCovered, PairsTotal int
}

// WorkflowRows lists the workflows worst first: the lowest branch coverage,
// then the most outcomes missed; workflows with no decisions to test last.
func (r Result) WorkflowRows() []WorkflowBranchRow {
	set := r.BranchSettings
	if set.TargetGrade == "" {
		set = scope.DefaultBranches()
	}
	rows := make([]WorkflowBranchRow, 0, len(r.Workflows))
	for _, w := range r.Workflows {
		row := WorkflowBranchRow{Name: w.Name, BranchSummary: w.BranchCov, Percent: w.BranchCov.Percent(), Missed: w.BranchCov.Missed(),
			PairsCovered: w.Pairs.Covered, PairsTotal: w.Pairs.Total}
		row.Grade = set.Grades.Grade(row.Percent)
		if w.BranchCov.Possible == 0 {
			row.Grade = NoDecisions
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Possible == 0) != (b.Possible == 0) {
			return b.Possible == 0
		}
		if a.Percent != b.Percent {
			return a.Percent < b.Percent
		}
		return a.Missed > b.Missed
	})
	return rows
}
