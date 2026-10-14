package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// listBuiltFromList marks a path in which a loop that builds a list (one
// append per trip) is skipped while a later loop over that list runs, or
// the other way round. The extractor proves how the list is built
// (internal/model/facts_builtlist.go); this detector only compares the
// facts on one path.
type listBuiltFromList struct{}

func (listBuiltFromList) Name() string     { return "list-built-from-list" }
func (listBuiltFromList) Labels() []string { return []string{"same-list-loops"} }

func (listBuiltFromList) Detect(p model.Path) (Classification, bool) {
	skipped, ran, ok := skippedAndRan(p, model.ListBuiltFrom)
	if !ok {
		return Classification{}, false
	}
	c := DefaultTestable()
	c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "same-list-loops", Certain, false
	c.Explanation = fmt.Sprintf("%s (%s) is skipped but %s (%s) runs; one of them builds the list the other walks, adding one element on every trip, so the list is empty exactly when the first loop does not run.",
		skipped.ID, skipped.Label, ran.ID, ran.Label)
	return c, true
}
