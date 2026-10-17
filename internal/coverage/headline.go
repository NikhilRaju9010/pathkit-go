package coverage

// Headline is the project's three headline numbers when some workflow has more
// paths than the list holds. Listed-path coverage of such a workflow (0 of 2000
// listed, say) says little, so it is left out of the path number; the
// workflow is measured by its stages and branches instead.
type Headline struct {
	// TooBig is how many workflows are too big for path coverage.
	TooBig int
	// PathCovered and PathTotal: covered paths over the paths a test can run,
	// in the workflows under the cap (the testable figure; the overall one
	// when classification is off, where every path is testable). Workflows is
	// how many workflows that is.
	PathCovered, PathTotal, PathWorkflows int
	// StageCovered and StageTotal: covered stage paths over the stage paths not
	// shown impossible, in the workflows over the cap. StagesTooBig stages are
	// too big to list and left out.
	StageCovered, StageTotal, StagesTooBig int
	// BranchCovered and BranchTotal: exits taken over exits, in every
	// workflow, over the whole map (not only the listed paths).
	BranchCovered, BranchTotal int
}

// Headline computes the headline numbers. ok is false when no workflow is over
// the cap; the headline is then the plain project totals.
func (r Result) Headline() (h Headline, ok bool) {
	for _, w := range r.Workflows {
		if w.Capped {
			ok = true
			h.TooBig++
			for _, s := range w.Stages {
				if s.Big {
					h.StagesTooBig++
					continue
				}
				h.StageCovered += s.Covered
				h.StageTotal += len(s.Paths) - s.CannotBeTested
			}
			h.BranchCovered += w.AllBranchesTaken
			h.BranchTotal += w.AllBranches
			continue
		}
		t := w.Breakdown.TestableCoverage()
		h.PathCovered += t.Covered
		h.PathTotal += t.Paths
		h.PathWorkflows++
		h.BranchCovered += w.BranchesTaken
		h.BranchTotal += w.Branches
	}
	return h, ok
}
