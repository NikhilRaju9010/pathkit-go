package classify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/facts"
)

// AssumptionJSON is the assumption that Temporal's default JSON converter
// decodes the workflow's inputs, signals, activity results and SideEffect
// results (see Definitions).
const AssumptionJSON = facts.AssumptionJSON

// AssumptionInterceptor is the assumption that no workflow interceptor in the
// module sees the original arguments of workflow.ExecuteActivity,
// ExecuteChildWorkflow and SignalExternalWorkflow, which the SDK encodes when
// the call is made (see Definitions).
const AssumptionInterceptor = facts.AssumptionInterceptor

// AssumptionStatus says whether an assumption is on in this run, why not
// when it is off, and how many claims rest on it.
type AssumptionStatus struct {
	Name   string
	On     bool
	Why    string // when off
	Claims int
}

// Assumptions lists the assumptions that matter in this run: one that is off
// while a workflow has a decoded value it would have been about (or that the
// config turned off), so every report says so, and one that claims rest on. final is
// every path's classification after coverage has run.
func (e *Engine) Assumptions(final []Classification) []AssumptionStatus {
	if e.engineOff {
		return nil
	}
	count := func(name string) int {
		n := 0
		for _, c := range final {
			if c.Bucket == CannotBeTested {
				for _, a := range c.Assumptions {
					if a == name {
						n++
					}
				}
			}
		}
		return n
	}
	var out []AssumptionStatus
	// Off because of the module: say so only when a workflow has a value the
	// assumption would have been about. (Turned off in the config: always say
	// so, the person asked for it.)
	add := func(name string, on bool, why string, relevant bool, mode string) {
		n := count(name)
		if (on && n == 0) || (!on && !relevant && mode != "off") {
			return
		}
		out = append(out, AssumptionStatus{Name: name, On: on, Why: why, Claims: n})
	}
	on, why := e.JSONConverterOn()
	add(AssumptionJSON, on, why, e.jsonRelevant, e.jsonMode)
	on, why = e.InterceptorOn()
	add(AssumptionInterceptor, on, why, e.icptRelevant, e.icptMode)
	return out
}

// assumptionNotes are the lines of the classification block about
// assumptions.
func (e *Engine) assumptionNotes(final []Classification) []string {
	var out []string
	for _, a := range e.Assumptions(final) {
		d, _ := Lookup(KindAssumption, a.Name)
		if a.On {
			out = append(out, fmt.Sprintf("Assumption %s: on; %s (%s)", a.Name, claimsRest(a.Claims), d.Meaning))
		} else {
			what := "no claim about decoded values is made"
			if a.Name == AssumptionInterceptor {
				what = "a slice or map handed to an activity, child workflow or signal is not followed"
			}
			out = append(out, fmt.Sprintf("Assumption %s: OFF, because %s; %s", a.Name, a.Why, what))
		}
	}
	return out
}

// groupNotes list the needs-review paths of unknown-input-type by the type
// switch they take a case of, biggest group first.
func groupNotes(final []Classification) []string {
	counts := map[string]int{}
	total := 0
	for _, c := range final {
		if c.Label == "unknown-input-type" && c.Bucket == NeedsReview && c.Group != "" {
			counts[c.Group]++
			total++
		}
	}
	if total == 0 {
		return nil
	}
	type g struct {
		name string
		n    int
	}
	var gs []g
	for k, v := range counts {
		gs = append(gs, g{k, v})
	}
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].n != gs[j].n {
			return gs[i].n > gs[j].n
		}
		return gs[i].name < gs[j].name
	})
	var parts []string
	for i, x := range gs {
		if i == 5 {
			parts = append(parts, fmt.Sprintf("and %d more switches", len(gs)-5))
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", x.name, pathsN(x.n)))
	}
	return []string{fmt.Sprintf("unknown-input-type: %s take a type switch case no test has run, on a value PathKit cannot follow; by switch: %s", pathsN(total), strings.Join(parts, ", "))}
}

func claimsRest(n int) string {
	if n == 1 {
		return "1 claim rests on it"
	}
	return fmt.Sprintf("%d claims rest on it", n)
}
