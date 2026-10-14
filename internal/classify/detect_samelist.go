package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// sameListLoops marks a path in which one loop is skipped (its list was
// empty) and another loop over the same list runs, or the other way round.
// "The same list" is decided by the extractor (the list value cannot
// change: a slice, array or string, never a map).
//
// A loop was skipped when the path takes its `exit` without ever taking its
// `iterate`; it ran when the path takes its `iterate`.
type sameListLoops struct{}

func (sameListLoops) Name() string     { return "same-list-loops" }
func (sameListLoops) Labels() []string { return []string{"same-list-loops"} }

func (sameListLoops) Detect(p model.Path) (Classification, bool) {
	skipped, ran, ok := skippedAndRan(p, model.SameList)
	if !ok {
		return Classification{}, false
	}
	c := DefaultTestable()
	c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "same-list-loops", Certain, false
	c.Explanation = fmt.Sprintf("%s (%s) is skipped but %s (%s) runs; both walk the same list, which cannot change between them, so they run the same number of times.",
		skipped.ID, skipped.Label, ran.ID, ran.Label)
	return c, true
}

// skippedAndRan finds, on one path, two loops that carry a fact of this
// kind with the same key, where one was skipped (the path takes its `exit`
// without ever taking its `iterate`) and the other ran (it takes its
// `iterate`).
func skippedAndRan(p model.Path, kind model.FactKind) (skipped, ran *model.Junction, ok bool) {
	iterated := map[*model.Junction]bool{}
	for _, s := range p.Steps {
		if s.Exit.Label == "iterate" {
			iterated[s.Junction] = true
		}
	}
	type loop struct {
		j       *model.Junction
		entered bool
	}
	byKey := map[string][]loop{}
	var keys []string // first-seen order, so the answer does not depend on map order
	for _, s := range p.Steps {
		j := s.Junction
		if s.Exit.Label != "iterate" && s.Exit.Label != "exit" {
			continue
		}
		if s.Exit.Label == "exit" && iterated[j] {
			continue // it ran, then ran out: counted at its iterate step
		}
		for _, f := range j.Facts {
			if f.Kind == kind {
				if _, seen := byKey[f.Key]; !seen {
					keys = append(keys, f.Key)
				}
				byKey[f.Key] = append(byKey[f.Key], loop{j, iterated[j]})
			}
		}
	}
	for _, k := range keys {
		group := byKey[k]
		for i := range group {
			for n := i + 1; n < len(group); n++ {
				a, b := group[i], group[n]
				if a.j == b.j || a.entered == b.entered {
					continue
				}
				if a.entered {
					a, b = b, a
				}
				return a.j, b.j, true
			}
		}
	}
	return nil, nil, false
}
