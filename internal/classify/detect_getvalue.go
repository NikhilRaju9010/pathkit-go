package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// contradictoryGetResult marks a path that takes two decisions which
// together say a value Temporal filled in with Get(ctx, &res) is both true
// and false (internal/model/facts_getvalue.go decides what is known at each
// exit; this detector only compares the facts on one path).
type contradictoryGetResult struct{}

func (contradictoryGetResult) Name() string     { return "contradictory-get-result" }
func (contradictoryGetResult) Labels() []string { return []string{"contradictory"} }

func (contradictoryGetResult) Detect(p model.Path) (Classification, bool) {
	type known struct {
		j     *model.Junction
		exit  string
		value bool
		text  string
	}
	byKey := map[string][]known{}
	var keys []string
	for _, s := range p.Steps {
		for _, f := range s.Junction.Facts {
			if f.Kind != model.ValueKnown || f.Exit != s.Exit.Label {
				continue
			}
			if _, seen := byKey[f.Key]; !seen {
				keys = append(keys, f.Key)
			}
			byKey[f.Key] = append(byKey[f.Key], known{s.Junction, s.Exit.Label, !f.Negated, f.Detail})
		}
	}
	for _, k := range keys {
		g := byKey[k]
		for i := range g {
			for n := i + 1; n < len(g); n++ {
				a, b := g[i], g[n]
				if a.j == b.j || a.value == b.value {
					continue
				}
				c := DefaultTestable()
				c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "contradictory", Certain, false
				c.Explanation = fmt.Sprintf("%s (%s) --%s--> needs the value to be %v and %s (%s) --%s--> needs it to be %v; it is filled in once by Get and nothing changes it, so it cannot give both answers.",
					a.j.ID, a.j.Label, a.exit, a.value, b.j.ID, b.j.Label, b.exit, b.value)
				return c, true
			}
		}
	}
	return Classification{}, false
}
