package classify

import (
	"fmt"
	"math/big"

	"github.com/NikhilRaju9010/pathkit-go/internal/facts"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// StageClass is one stage path and what the engine says about it.
type StageClass struct {
	Path  model.StagePath
	Class Classification
}

// StagePlan is a workflow cut into stages, with every small stage's paths
// classified by the fact engine (docs/DESIGN.md D16).
type StagePlan struct {
	Stages []model.Stage
	// Classes has the paths of each stage (empty for a stage that is too big to
	// list, or when the engine is off).
	Classes [][]StageClass
	// Contexts is how many incoming states each stage was examined in; CutOff
	// says there were more.
	Contexts []int
	CutOff   []bool
}

// PlanStages cuts the workflow into stages and classifies the paths of every
// stage one stage at a time. The answer is kept: asking again is free.
func (e *Engine) PlanStages(workflow string, g *model.Graph) *StagePlan {
	if p, ok := e.plans[workflow]; ok {
		return p
	}
	stages := g.Stages(e.stageMax)
	plan := &StagePlan{Stages: stages, Classes: make([][]StageClass, len(stages)), Contexts: make([]int, len(stages)), CutOff: make([]bool, len(stages))}
	var reports []facts.StageReport
	if an := e.facts[workflow]; an != nil && !e.engineOff {
		reports = an.AnalyzeStages(g, stages, e.maxContexts)
	}
	for k, st := range stages {
		if st.Big {
			continue
		}
		var rep *facts.StageReport
		if reports != nil {
			rep = &reports[k]
			plan.Contexts[k], plan.CutOff[k] = rep.Contexts, rep.CutOff
		}
		paths, _ := g.StagePaths(st, int(st.Count.Int64())+1)
		for i, sp := range paths {
			c := DefaultTestable()
			if rep != nil && i < len(rep.Results) {
				c = e.stageClass(rep.Results[i], rep)
			}
			// A stage path through the old side of a GetVersion is labelled
			// like a listed path through it (it stays testable-or-special:
			// the stage counts do not move).
			if c.Bucket == Testable && e.detectorOn("version-gate-old-branch") {
				if vc, ok := (versionGateOldBranch{}).Detect(model.Path{Steps: sp.Steps}); ok {
					c = vc
				}
			}
			plan.Classes[k] = append(plan.Classes[k], StageClass{Path: sp, Class: c})
		}
	}
	e.plans[workflow] = plan
	return plan
}

// stageClass puts one stage path in a bucket.
func (e *Engine) stageClass(r facts.StageResult, rep *facts.StageReport) Classification {
	c := DefaultTestable()
	switch r.Verdict {
	case facts.Impossible:
		if !e.detectorOn("value-flow") || r.Claim == nil {
			return c
		}
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, string(r.Claim.Kind), Certain, false
		c.Source, c.Detector = SourceAuto, "value-flow"
		c.Explanation = r.Claim.Explanation
		if r.Contexts > 1 {
			c.Explanation += fmt.Sprintf(" The same holds in each of the %d different states the stage can start in.", r.Contexts)
		}
		c.Proof = convertProof(r.Claim.Proof)
		c.Assumptions = r.Claim.Assumptions
	case facts.Pending:
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "model-limit", Likely, true
		c.Source, c.Detector = SourceAuto, "value-flow"
		c.Explanation = fmt.Sprintf("A decision on this stage path cannot be taken in any of the %d different states the stage was examined in, but the stage can start in more states than classification.engine.max_contexts follows, so a state that allows it may exist.", r.Contexts)
	default:
		if r.Defect != nil && e.detectorOn("likely-defect") {
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "likely-defect", Likely, true
			c.Source, c.Detector = SourceAuto, "likely-defect"
			c.Explanation = r.Defect.Explanation
			c.Proof = convertProof(r.Defect.Proof)
		} else if r.Suspect != nil && e.detectorOn("possible-correlation") {
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "possible-correlation", Likely, true
			c.Source, c.Detector = SourceAuto, "possible-correlation"
			c.Explanation = r.Suspect.Explanation
		} else if r.Unknown != nil && e.detectorOn("unknown-input-type") {
			c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "unknown-input-type", Likely, true
			c.Source, c.Detector = SourceAuto, "unknown-input-type"
			c.Explanation = r.Unknown.Explanation
			c.SuspectExit, c.Group = r.Unknown.Exit, r.Unknown.Group
		}
	}
	return c
}

// A PrunedClaim is the engine's reason for leaving a part of the map out of
// the path list.
type PrunedClaim struct {
	Group model.PrunedGroup
	Class Classification
}

// Listing is a workflow's path list with the impossible parts left out and
// counted.
type Listing struct {
	model.PathList
	// Exact is the exact number of paths of the workflow.
	Exact  *big.Int
	Pruned []PrunedClaim
}

// Listing lists the workflow's paths. A workflow with no more paths than the
// path cap is listed whole. A bigger one is listed with the parts the engine
// shows impossible left out and counted (each as one group with its proof), so
// the cap fills with paths that can be taken. The listed paths and their IDs
// are the same ones a plain listing has.
func (e *Engine) Listing(workflow string, g *model.Graph) *Listing {
	if l, ok := e.listings[workflow]; ok {
		return l
	}
	l := &Listing{Exact: g.PathCount()}
	an := e.facts[workflow]
	if l.Exact.Cmp(big.NewInt(int64(e.pathCap))) <= 0 || an == nil || e.engineOff || an.Off() != "" || !e.detectorOn("value-flow") {
		l.PathList = g.Paths(e.pathCap)
	} else {
		pl := g.PathsPruned(e.pathCap,
			func() any { return an.NewWalk() },
			func(s any) any { return s.(*facts.Walk).Clone() },
			func(s any, st model.Step) any {
				if c := s.(*facts.Walk).Step(st); c != nil {
					return c
				}
				return nil
			})
		l.PathList = pl.PathList
		for _, gr := range pl.Pruned {
			cl := e.stageClass(facts.StageResult{Verdict: facts.Impossible, Claim: gr.Info.(*facts.Claim), Contexts: 1}, nil)
			cl.UsesStep = gr.Info.(*facts.Claim).Uses
			l.Pruned = append(l.Pruned, PrunedClaim{Group: gr, Class: cl})
		}
	}
	e.listings[workflow] = l
	return l
}

// PlainPlan cuts the workflow into stages without asking the fact engine:
// every stage path is testable. It is what the reports use when
// classification is off.
func PlainPlan(g *model.Graph, maxPaths int) *StagePlan {
	stages := g.Stages(maxPaths)
	plan := &StagePlan{Stages: stages, Classes: make([][]StageClass, len(stages)), Contexts: make([]int, len(stages)), CutOff: make([]bool, len(stages))}
	for k, st := range stages {
		if st.Big {
			continue
		}
		paths, _ := g.StagePaths(st, int(st.Count.Int64())+1)
		for _, sp := range paths {
			plan.Classes[k] = append(plan.Classes[k], StageClass{Path: sp, Class: DefaultTestable()})
		}
	}
	return plan
}

// PlainListing lists the workflow's paths up to the cap, with nothing pruned.
func PlainListing(g *model.Graph) *Listing {
	return &Listing{PathList: g.Paths(model.DefaultMaxPaths), Exact: g.PathCount()}
}
