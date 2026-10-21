package coverage

// DecisionCheck is the result of "pathkit check-decisions": for decisions a
// test covered, whether the tests fail when the decision goes the other way
// (milestone M11, B8). It is read from .pathkit/decision-check.json and is
// nil when no check was run.
type DecisionCheck struct {
	// Date is when the check ran (YYYY-MM-DD).
	Date string `json:"date"`
	// Checked decisions, and what became of them.
	Checked  int `json:"checked"`
	Noticed  int `json:"noticed"`
	Survived int `json:"survived"`
	NoEffect int `json:"noVisibleEffect"`
	Flaky    int `json:"flaky"`
	// Survivors are the decisions that went the other way without a test failing.
	Survivors []DecisionSurvivor `json:"survivors,omitempty"`
	// Mutants is every decision forced, one entry each, with what became of it.
	Mutants []DecisionMutant `json:"mutants,omitempty"`
	// Skipped says what was not checked and why (a package whose tests fail
	// before any change, a decision that cannot be forced).
	Skipped []string `json:"skipped,omitempty"`
}

// DecisionMutant is one decision forced to one outcome and the tests re-run.
type DecisionMutant struct {
	Branch   string   `json:"branch"`
	Workflow string   `json:"workflow"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Decision string   `json:"decision"`
	Outcome  string   `json:"outcome"`
	Tests    []string `json:"tests,omitempty"`
	// Result is "noticed", "survived", "no visible effect", "flaky" or "error".
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// DecisionSurvivor is one decision no test noticed.
type DecisionSurvivor struct {
	Branch   string `json:"branch"` // the branch ID that was forced
	Workflow string `json:"workflow"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Decision string `json:"decision"`
	Outcome  string `json:"outcome"` // the outcome that was forced
	Sentence string `json:"sentence"`
}

// Score is the share of checked decisions the tests noticed, in percent
// (decisions with no visible effect and flaky ones are not counted).
func (d DecisionCheck) Score() float64 {
	n := d.Noticed + d.Survived
	return Percent(d.Noticed, n)
}
