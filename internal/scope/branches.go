package scope

import "fmt"

// The grade names, lowest first. "needs work" is anything below the first band.
const (
	GradeNeedsWork   = "needs work"
	GradeAcceptable  = "acceptable"
	GradeCommendable = "commendable"
	GradeExemplary   = "exemplary"
)

// Grades are the lower edges, in percent, of the three grade bands of branch
// coverage. Below Acceptable is "needs work".
type Grades struct {
	Acceptable, Commendable, Exemplary float64
}

// DefaultGrades are the bands used when the config does not say.
func DefaultGrades() Grades { return Grades{Acceptable: 60, Commendable: 75, Exemplary: 90} }

// Grade names a percent.
func (g Grades) Grade(percent float64) string {
	switch {
	case percent >= g.Exemplary:
		return GradeExemplary
	case percent >= g.Commendable:
		return GradeCommendable
	case percent >= g.Acceptable:
		return GradeAcceptable
	}
	return GradeNeedsWork
}

// Floor is the lower edge of a grade given by name.
func (g Grades) Floor(grade string) float64 {
	switch grade {
	case GradeExemplary:
		return g.Exemplary
	case GradeCommendable:
		return g.Commendable
	case GradeAcceptable:
		return g.Acceptable
	}
	return 0
}

// Branches is the "branches" section of the config: the grade bands of branch
// coverage and the grade a team aims for.
type Branches struct {
	Grades Grades
	// TargetGrade is the grade a workflow should reach ("commendable" unless
	// the config says);
	TargetGrade string
}

// DefaultBranches is the section when the config has none.
func DefaultBranches() Branches {
	return Branches{Grades: DefaultGrades(), TargetGrade: GradeCommendable}
}

type branchesPart struct {
	Grades      *gradesPart `json:"grades"`
	TargetGrade *string     `json:"target_grade"`
}

type gradesPart struct {
	Acceptable  *float64 `json:"acceptable"`
	Commendable *float64 `json:"commendable"`
	Exemplary   *float64 `json:"exemplary"`
}

func readBranches(p *branchesPart, fail func(string, ...any) error) (Branches, error) {
	b := DefaultBranches()
	if p == nil {
		return b, nil
	}
	if g := p.Grades; g != nil {
		if g.Acceptable != nil {
			b.Grades.Acceptable = *g.Acceptable
		}
		if g.Commendable != nil {
			b.Grades.Commendable = *g.Commendable
		}
		if g.Exemplary != nil {
			b.Grades.Exemplary = *g.Exemplary
		}
		if !(b.Grades.Acceptable > 0 && b.Grades.Acceptable < b.Grades.Commendable && b.Grades.Commendable < b.Grades.Exemplary && b.Grades.Exemplary <= 100) {
			return b, fail(`branches.grades must satisfy 0 < acceptable < commendable < exemplary <= 100 (now %v, %v, %v)`, b.Grades.Acceptable, b.Grades.Commendable, b.Grades.Exemplary)
		}
	}
	if p.TargetGrade != nil {
		switch *p.TargetGrade {
		case GradeAcceptable, GradeCommendable, GradeExemplary:
			b.TargetGrade = *p.TargetGrade
		default:
			return b, fail(`branches.target_grade must be "acceptable", "commendable" or "exemplary", not %s`, fmt.Sprintf("%q", *p.TargetGrade))
		}
	}
	return b, nil
}

// BranchSettings are the grade bands and target grade to use: the config's, or
// the defaults when there is no config or it has no "branches" section.
func (c *Config) BranchSettings() Branches {
	if c == nil || c.Branches.Grades == (Grades{}) || c.Branches.TargetGrade == "" {
		return DefaultBranches()
	}
	return c.Branches
}
