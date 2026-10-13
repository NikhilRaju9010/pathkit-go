package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// constantCondition marks a path that takes the side of an `if` that the
// compiler knows can never run (`if false`, `if debug` with a const). Which
// conditions count as constant is decided by the extractor
// (internal/model/facts_const.go); this detector only reads its facts.
type constantCondition struct{}

func (constantCondition) Name() string     { return "constant-condition" }
func (constantCondition) Labels() []string { return []string{"constant-condition"} }

func (constantCondition) Detect(p model.Path) (Classification, bool) {
	for _, s := range p.Steps {
		for _, f := range s.Junction.Facts {
			if f.Kind != model.ConstCondition || f.Exit != s.Exit.Label {
				continue
			}
			c := DefaultTestable()
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "constant-condition", Certain, false
			c.Explanation = fmt.Sprintf("%s (%s) takes the %s side, but %s, so that side can never run.", s.Junction.ID, s.Junction.Label, s.Exit.Label, f.Detail)
			return c, true
		}
	}
	return Classification{}, false
}
