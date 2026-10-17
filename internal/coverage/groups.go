package coverage

import (
	"path/filepath"
	"sort"
	"strconv"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// MissedGroup is a set of missed paths that all leave the road the tests
// walked at the same step. The tests reached the decision (Prefix is the
// road they walked to it) but no test took the exit Step.Exit, so every path
// in the group is missed for that one reason, and one new test that takes
// the exit usually covers several of them.
type MissedGroup struct {
	Workflow string
	// Prefix is the steps that at least one test walked, up to the decision.
	Prefix []model.Step
	// Step is the first step no test took. Its Exit is nil when the paths
	// differ from a tested path only in how they end (End says how).
	Step model.Step
	End  model.EndKind
	// Missed is how many testable paths no test ran are in the group (among
	// the listed paths of the workflow).
	Missed int
	// NoTests: no test ran any path of the workflow, so the tests did not
	// reach the decision either (Prefix is empty).
	NoTests bool
	// Example is the first of those paths, to build the hint from.
	Example model.Path
}

// MissedGroups groups a workflow's missed testable paths by the first step
// no test took. It builds a prefix tree of the paths the tests ran: a missed
// path follows the tree as far as it goes, and the step where it leaves the
// tree is its group. With no covered path the tree is only the start, so the
// groups are the first decisions' exits. Biggest groups first; groups of the
// same size keep the order of their first path.
func MissedGroups(w WorkflowResult) []MissedGroup {
	// The tree is every prefix of every path a test ran, beyond the list
	// too (w.Ran). A result built by hand without it falls back to the
	// covered listed paths.
	tree := map[string]bool{"": true}
	noTests := len(w.Ran) == 0
	if w.Ran != nil {
		for k := range w.Ran {
			tree[k] = true
		}
	} else {
		for _, p := range w.Paths {
			if !p.Covered {
				continue
			}
			noTests = false
			key := ""
			for _, s := range p.Path.Steps {
				key += s.Exit.ID.String() + "\n"
				tree[key] = true
			}
		}
	}
	var order []string
	byKey := map[string]*MissedGroup{}
	for _, p := range w.Paths {
		if p.Covered || p.Class.Bucket != classify.Testable {
			continue
		}
		key, k := "", 0
		for ; k < len(p.Path.Steps); k++ {
			next := key + p.Path.Steps[k].Exit.ID.String() + "\n"
			if !tree[next] {
				break
			}
			key = next
		}
		g := key
		if k < len(p.Path.Steps) {
			g = key + p.Path.Steps[k].Exit.ID.String() + "\n"
		} else {
			g = key + "end:" + string(p.Path.End)
		}
		grp := byKey[g]
		if grp == nil {
			grp = &MissedGroup{Workflow: w.Name, Prefix: p.Path.Steps[:k], Example: p.Path, NoTests: noTests}
			if k < len(p.Path.Steps) {
				grp.Step = p.Path.Steps[k]
			} else {
				grp.End = p.Path.End
			}
			byKey[g] = grp
			order = append(order, g)
		}
		grp.Missed++
	}
	out := make([]MissedGroup, 0, len(order))
	for _, g := range order {
		out = append(out, *byKey[g])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Missed > out[j].Missed })
	return out
}

// AllMissedGroups is MissedGroups for every workflow, biggest first (a tie
// keeps the workflow order, then the order inside the workflow).
func AllMissedGroups(res Result) []MissedGroup {
	var out []MissedGroup
	for _, w := range res.Workflows {
		out = append(out, MissedGroups(w)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Missed > out[j].Missed })
	return out
}

// Where is the decision's place as "file:line" ("" when the junction has no
// position).
func (g MissedGroup) Where() string {
	if g.Step.Junction == nil || g.Step.Junction.Pos.Filename == "" {
		return ""
	}
	return filepath.Base(g.Step.Junction.Pos.Filename) + ":" + strconv.Itoa(g.Step.Junction.Pos.Line)
}
