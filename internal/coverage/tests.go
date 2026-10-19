package coverage

import (
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// NoTestName is the row for traces that say nothing about the test.
const NoTestName = "(test name not recorded)"

// TestResult is what one Go test covered: how many of its runs counted, which
// workflows it ran, and the scenarios and branches (exits of decisions) its
// runs took. Unique ones are taken by no other test.
type TestResult struct {
	Name      string
	Traces    int
	Workflows []string
	Scenarios int
	Branches  int
	// UniqueScenarios and UniqueBranches are the ones only this test covers.
	UniqueScenarios, UniqueBranches int
	// AddsNothing: every scenario and branch of the test is covered by another
	// test too (and some other test names are recorded). Never set for the row
	// of traces without a test name.
	AddsNothing bool
}

// testAcc collects what each test covered while the traces are read.
type testAcc struct {
	traces    int
	workflows map[string]bool
	scenarios map[string]bool
	branches  map[string]bool
}

type testObserver map[string]*testAcc

func (o testObserver) add(name, workflow, scenario string, p model.Path) {
	if name == "" {
		name = NoTestName
	}
	a := o[name]
	if a == nil {
		a = &testAcc{workflows: map[string]bool{}, scenarios: map[string]bool{}, branches: map[string]bool{}}
		o[name] = a
	}
	a.traces++
	a.workflows[workflow] = true
	a.scenarios[workflow+"|"+scenario] = true
	for _, s := range p.Steps {
		a.branches[workflow+"|"+s.Exit.ID.String()] = true
	}
}

// results turns the observations into rows, sorted by name, the row without a
// name last.
func (o testObserver) results() []TestResult {
	scenCount, branchCount := map[string]int{}, map[string]int{}
	for _, a := range o {
		for k := range a.scenarios {
			scenCount[k]++
		}
		for k := range a.branches {
			branchCount[k]++
		}
	}
	named := 0
	for n := range o {
		if n != NoTestName {
			named++
		}
	}
	var out []TestResult
	for n, a := range o {
		r := TestResult{Name: n, Traces: a.traces, Scenarios: len(a.scenarios), Branches: len(a.branches)}
		for w := range a.workflows {
			r.Workflows = append(r.Workflows, w)
		}
		sort.Strings(r.Workflows)
		for k := range a.scenarios {
			if scenCount[k] == 1 {
				r.UniqueScenarios++
			}
		}
		for k := range a.branches {
			if branchCount[k] == 1 {
				r.UniqueBranches++
			}
		}
		r.AddsNothing = n != NoTestName && r.UniqueScenarios == 0 && r.UniqueBranches == 0
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == NoTestName) != (out[j].Name == NoTestName) {
			return out[j].Name == NoTestName
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// TookExits are the exits (by id) some counted trace took, read from the
// prefixes in Ran.
func (w WorkflowResult) TookExits() map[string]bool {
	out := map[string]bool{}
	for k := range w.Ran {
		lines := strings.Split(strings.TrimSuffix(k, "\n"), "\n")
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			out[lines[len(lines)-1]] = true
		}
	}
	return out
}
