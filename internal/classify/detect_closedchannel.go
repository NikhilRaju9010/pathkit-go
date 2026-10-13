package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// closedSignalChannel marks a path that takes the "closed" side of a
// `more` check on a channel from workflow.GetSignalChannel. Temporal feeds
// that channel and never closes it, and nothing in the SDK or the Go test
// environment closes it either (checked in the SDK source, v1.49.0). Only
// code can: a type assertion (the taint scan below finds it) or a custom
// worker interceptor that hands the workflow a channel it has closed
// (not looked for). That is an assessment, not a proof, so the definition
// says so.
//
// A channel the workflow made itself (NewChannel, NewBufferedChannel) may
// be closed by the workflow, so its closed side is reachable and the path
// stays testable. A channel whose origin PathKit cannot follow goes to
// needs-review: unknown is never "impossible".
type closedSignalChannel struct{}

func (closedSignalChannel) Name() string { return "closed-signal-channel" }

func (closedSignalChannel) Labels() []string {
	return []string{"closed-signal-channel", "needs-review"}
}

func (closedSignalChannel) Detect(p model.Path) (Classification, bool) {
	var review *Classification
	for _, s := range p.Steps {
		for _, f := range s.Junction.Facts {
			if f.Kind != model.MoreCheck || f.Exit != s.Exit.Label {
				continue
			}
			switch f.Origin {
			case model.OriginSignal:
				c := DefaultTestable()
				c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, "closed-signal-channel", Certain, false
				name := ""
				if f.Name != "" {
					name = fmt.Sprintf(" %q", f.Name)
				}
				c.Explanation = fmt.Sprintf("%s (%s) takes the side where the channel from workflow.GetSignalChannel%s is closed (more == false). The Temporal SDK and server do not close signal channels; only a custom worker interceptor could.", s.Junction.ID, s.Junction.Label, name)
				return c, true
			case model.OriginUnknown:
				if review == nil {
					c := DefaultTestable()
					c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "needs-review", Likely, true
					c.Explanation = fmt.Sprintf("%s (%s) takes the closed side of a channel receive, but PathKit could not follow the channel back to the call that made it, so it can't tell whether the workflow closes it.", s.Junction.ID, s.Junction.Label)
					review = &c
				}
			}
		}
	}
	if review != nil {
		return *review, true
	}
	return Classification{}, false
}
