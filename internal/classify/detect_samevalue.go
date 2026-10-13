package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// contradictory marks a path that takes two decisions which test the same
// value, but gives opposite answers: `if valid {...}` and later
// `if !valid {...}` with `valid` unchanged. The extractor decides what
// "the same value, unchanged" means (SSA, internal/model/facts_ssa.go); this
// detector only compares the facts on one path.
type contradictory struct{}

func (contradictory) Name() string     { return "contradictory" }
func (contradictory) Labels() []string { return []string{"contradictory"} }

func (contradictory) Detect(p model.Path) (Classification, bool) {
	type seen struct {
		j    *model.Junction
		exit string
		neg  bool
	}
	byKey := map[string][]seen{}
	for _, s := range p.Steps {
		if s.Exit.Label != "true" && s.Exit.Label != "false" {
			continue
		}
		for _, f := range s.Junction.Facts {
			if f.Kind == model.SameValue {
				byKey[f.Key] = append(byKey[f.Key], seen{s.Junction, s.Exit.Label, f.Negated})
			}
		}
	}
	for _, group := range byKey {
		for i := range group {
			for k := i + 1; k < len(group); k++ {
				a, b := group[i], group[k]
				if a.j == b.j {
					continue
				}
				// The value is true when the written condition is true
				// (not negated) or false (negated).
				va := (a.exit == "true") != a.neg
				vb := (b.exit == "true") != b.neg
				if va == vb {
					continue
				}
				c := DefaultTestable()
				c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "contradictory", Certain, false
				c.Explanation = fmt.Sprintf("%s (%s) --%s--> and %s (%s) --%s--> test the same value, which cannot change between them, so they cannot give these two answers.",
					a.j.ID, a.j.Label, a.exit, b.j.ID, b.j.Label, b.exit)
				return c, true
			}
		}
	}
	return Classification{}, false
}
