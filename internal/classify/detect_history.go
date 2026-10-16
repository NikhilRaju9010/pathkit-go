package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// historySuggested marks a path taken when the SDK suggests Continue-As-New
// because the history is large. A normal test never gets that suggestion,
// but the SDK's test environment can fake it
// (env.SetContinueAsNewSuggested(true), SetCurrentHistoryLength,
// SetCurrentHistorySize), so it is a special test, not impossible.
//
// A comparison of the history length or size with a number is only a hint:
// PathKit cannot tell the number is a real limit, so those go to
// needs-review.
type historySuggested struct{}

func (historySuggested) Name() string     { return "history-limit" }
func (historySuggested) Labels() []string { return []string{"history-limit", "needs-review"} }

func (historySuggested) Detect(p model.Path) (Classification, bool) {
	var weak *Classification
	for _, s := range p.Steps {
		for _, f := range s.Junction.Facts {
			if f.Exit != s.Exit.Label {
				continue
			}
			switch f.Kind {
			case model.HistorySuggested:
				c := DefaultTestable()
				c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsSpecialTest, "history-limit", Certain, true
				c.Explanation = fmt.Sprintf("%s (%s) is the side taken when the SDK suggests Continue-As-New. Fake it in a test with env.SetContinueAsNewSuggested(true).", s.Junction.ID, s.Junction.Label)
				c.Proof = []ProofStep{
					stepProof(p, s.Junction, fmt.Sprintf("%s is the side taken when the SDK suggests Continue-As-New.", where(s.Junction))),
					conclusion("A normal test is never given that suggestion; the test environment can fake it."),
				}
				return c, true
			case model.HistorySize:
				if weak == nil {
					c := DefaultTestable()
					c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "needs-review", Likely, true
					c.Explanation = fmt.Sprintf("%s (%s) compares the history length or size with a number. If that is a history limit, a test can fake it with env.SetCurrentHistoryLength or SetCurrentHistorySize; PathKit can't tell, so please review.", s.Junction.ID, s.Junction.Label)
					c.Proof = []ProofStep{
						stepProof(p, s.Junction, fmt.Sprintf("%s compares the history length or size with a number.", where(s.Junction))),
						conclusion("If that number is a history limit, a test can fake it; PathKit cannot tell, so a person should review it."),
					}
					weak = &c
				}
			}
		}
	}
	if weak != nil {
		return *weak, true
	}
	return Classification{}, false
}
