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
	n := 0
	for _, c := range final {
		if c.Bucket == CannotBeTested {
			for _, a := range c.Assumptions {
				if a == AssumptionJSON {
					n++
				}
			}
		}
	}
	if e.engineOff {
		return nil
	}
	on, why := e.JSONConverterOn()
	if on && n == 0 {
		return nil
	}
	// Off because the module sets its own converter: say so only when a
	// workflow has a decoded value the assumption would have been about.
	// (Turned off in the config: always say so, the person asked for it.)
	if !on && !e.jsonRelevant && e.jsonMode != "off" {
		return nil
	}
	return []AssumptionStatus{{Name: AssumptionJSON, On: on, Why: why, Claims: n}}
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
			out = append(out, fmt.Sprintf("Assumption %s: OFF, because %s; no claim about decoded values is made", a.Name, a.Why))
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
