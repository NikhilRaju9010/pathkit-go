package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// loopExitBeforeFirstTrip marks a path that leaves a loop by `exit` without
// ever running its body, when the extractor proved the loop's condition is
// true the first time it is tested (internal/model/facts_loopfirst.go).
type loopExitBeforeFirstTrip struct{}

func (loopExitBeforeFirstTrip) Name() string     { return "loop-exit-before-first-trip" }
func (loopExitBeforeFirstTrip) Labels() []string { return []string{"impossible-loop-exit"} }

func (loopExitBeforeFirstTrip) Detect(p model.Path) (Classification, bool) {
	iterated := map[*model.Junction]bool{}
	for _, s := range p.Steps {
		if s.Exit.Label == "iterate" {
			iterated[s.Junction] = true
		}
	}
	for _, s := range p.Steps {
		if s.Exit.Label != "exit" || iterated[s.Junction] {
			continue
		}
		for _, f := range s.Junction.Facts {
			if f.Kind != model.LoopFirstTripAlways {
				continue
			}
			c := DefaultTestable()
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "impossible-loop-exit", Certain, false
			c.Explanation = fmt.Sprintf("%s (%s) is left without running its body, but %s.", s.Junction.ID, s.Junction.Label, f.Detail)
			c.Proof = []ProofStep{
				stepProof(p, s.Junction, fmt.Sprintf("%s is left without running its body.", where(s.Junction))),
				conclusion(f.Detail + "; leaving it without a trip is therefore impossible."),
			}
			return c, true
		}
	}
	return Classification{}, false
}
