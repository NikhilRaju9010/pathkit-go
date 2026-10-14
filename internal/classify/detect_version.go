package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// versionGateOldBranch marks a path through the OLD side of a
// workflow.GetVersion check. New runs never take it (GetVersion returns the
// newest version), so a normal test cannot reach it; but the SDK's test
// environment can: env.OnGetVersion mocks the version. That makes it a
// special test, not an impossible path.
type versionGateOldBranch struct{}

func (versionGateOldBranch) Name() string     { return "version-gate-old-branch" }
func (versionGateOldBranch) Labels() []string { return []string{"version-gate-old-branch"} }

func (versionGateOldBranch) Detect(p model.Path) (Classification, bool) {
	for _, s := range p.Steps {
		for _, f := range s.Junction.Facts {
			if f.Kind != model.VersionCheck || f.Exit != s.Exit.Label {
				continue
			}
			c := DefaultTestable()
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsSpecialTest, "version-gate-old-branch", Certain, true
			c.Explanation = fmt.Sprintf("%s (%s) takes the old side of workflow.GetVersion(%q). Normal test runs get the newest version. Mock the version with env.OnGetVersion(%q, ...) to reach it.", s.Junction.ID, s.Junction.Label, f.Name, f.Name)
			return c, true
		}
	}
	return Classification{}, false
}
