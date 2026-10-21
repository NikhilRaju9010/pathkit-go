package htmlreport

import "fmt"

// A term is a word or number on the page that has a tooltip (an "i" marker)
// and a glossary entry: the plain name, one or two plain sentences, and the
// technical name (milestone M11, R3). The glossary at the bottom of the page
// lists them all, and the tooltip shows the same text, so a printed page
// (which has no tooltips) loses nothing.
type term struct {
	Key  string
	Term string // the plain name
	Text string // one or two plain sentences
	Tech string // the technical name ("" when it has none)
}

var terms = []term{
	{"branch-coverage", "Branch coverage", "The share of decision outcomes that can happen and that your tests took. It is the main number of this page.", "branch coverage"},
	{"outcome", "Decision outcome", "One way a decision can go: an if being true or false, an activity succeeding or failing, one case of a switch, a wait ending by signal or by timeout.", "branch, exit"},
	{"grade", "Grade", "A word for the percentage: needs work below 60%, acceptable from 60%, commendable from 75%, exemplary from 90%. Your team can change the limits.", "branches.grades"},
	{"can-be-tested", "Can be tested", "A test can make the workflow take this route.", "testable"},
	{"cant-happen", "Can't happen", "PathKit proved that nothing can make this happen, for example because an earlier value settles the decision. These outcomes are left out of the percentage and listed with the proof.", "cannot-be-tested"},
	{"special-test", "Needs a special test", "It can happen in production but an ordinary test cannot reach it, for example the old side of a version check, which needs a replay of an old history.", "needs-special-test"},
	{"needs-check", "Needs a person to check", "PathKit is not sure. It stays in the numbers until a person decides.", "needs-review"},
	{"known-bug", "Blocked by a known bug", "A person marked this route as blocked by a bug that is known. It stays in the percentage, on a line of its own, until the bug is fixed.", "known-defect"},
	{"possible-bug", "Possible bug", "The code tests for a plain value (a string, say) where the value is really a pointer to one, so the test can never be true. It is probably a mistake in the code, not a route that cannot happen.", "likely-defect"},
	{"two-step", "Two-step combinations", "An outcome of one decision followed by an outcome of the next. A finer measure than branch coverage.", "decision pairs"},
	{"condition-part", "Each part of a condition", "In a condition like a && b, each part (a, b) should be seen true and false when it is checked.", "condition coverage"},
	{"notice", "Tests notice wrong decisions", "PathKit changed a decision to go the other way and ran the tests that cover it. This is how often a test failed.", "decision check"},
	{"next", "What to test next", "The decision outcomes that can happen and that no test took, riskiest first.", "tests to write first"},
	{"risk", "Risk", "The riskiest thing a test of this outcome must do: cancel, undo (compensation), activity failure, continue as new, timer or timeout, signal or update, plain logic, or an old version.", "tag"},
	{"outcomes-tested", "Outcomes tested", "How many of the outcomes that can happen your tests took, out of all of them.", "branches covered / possible"},
	{"missed", "Missed", "Outcomes that can happen and that no test took.", ""},
	{"test-run", "Test run", "One run of a workflow recorded while your tests ran.", "trace"},
	{"unplaced-run", "Test run PathKit couldn't place", "A recorded run that does not fit any route PathKit drew. The drawing may be incomplete there, so PathKit trusts its \"can't happen\" proofs less around it.", "unmatched trace"},
	{"section", "Section of the workflow", "A long workflow is cut into parts that are counted one at a time.", "stage"},
	{"scenario", "Scenario", "A distinct list of things a workflow does outside itself (calls, timers, signals). Different routes that do the same things are one scenario.", "scenario"},
	{"path-id", "Path ID", "A short code that names one route through a workflow and stays the same when unrelated code changes.", "stableId"},
	{"left-out", "Left out (can't happen)", "Parts of a very large workflow that PathKit proved impossible and did not list one by one. They are counted exactly.", "pruned groups"},
	{"unread", "Code PathKit couldn't read", "Places PathKit did not follow, such as a call through an interface. A route through them has a scenario of its own and is never merged with another.", "unread panel"},
	{"assumptions", "What PathKit assumes", "Things PathKit takes for granted when it proves something can't happen, for example that Temporal's default JSON converter is used. Each can be turned off.", "assumptions"},
	{"path", "Path", "One complete route through a workflow, from the start to an end.", "path"},
	{"workflow", "Workflow", "One Temporal workflow function PathKit found.", ""},
	{"checklist", "Decision checklist", "Every decision of a workflow in plain words, with each outcome ticked when a test took it, crossed when none did, or marked when it can't happen.", ""},
	{"possible-bugs-card", "Possible bugs found", "The places where the code looks like it tests a pointer as if it were the plain value.", "likely-defect"},
}

func termByKey(key string) (term, error) {
	for _, t := range terms {
		if t.Key == key {
			return t, nil
		}
	}
	return term{}, fmt.Errorf("htmlreport: no glossary term %q", key)
}

// tipView is what the "tip" template needs.
type tipView struct{ Key, Term string }

func tipFor(key string) (tipView, error) {
	t, err := termByKey(key)
	if err != nil {
		return tipView{}, err
	}
	return tipView{Key: t.Key, Term: t.Term}, nil
}
