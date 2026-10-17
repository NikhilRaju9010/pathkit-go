package classify

import (
	"github.com/NikhilRaju9010/pathkit-go/internal/facts"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The fact engine (internal/facts) walks a path with a notebook of what it
// knows about the workflow's values. It needs the whole workflow graph, not
// just one path, so Engine.classify runs it itself (see engineClaim). These
// two entries give it the names the config uses in detectors.disable and
// keep the list of detectors and the list of labels in step; their Detect
// does nothing.

// valueFlow claims a path impossible because earlier code and decisions on
// the same path settle a later decision the other way.
type valueFlow struct{}

func (valueFlow) Name() string { return "value-flow" }
func (valueFlow) Labels() []string {
	return []string{"value-flow", "contradictory", "constant-condition", "impossible-type", "impossible-nil", "impossible-loop-exit"}
}
func (valueFlow) Detect(model.Path) (Classification, bool) { return Classification{}, false }

// possibleCorrelation sends an untested path to needs-review when it asks
// about the same value twice and PathKit had to forget the value in between.
type possibleCorrelation struct{}

func (possibleCorrelation) Name() string     { return "possible-correlation" }
func (possibleCorrelation) Labels() []string { return []string{"possible-correlation"} }
func (possibleCorrelation) Detect(model.Path) (Classification, bool) {
	return Classification{}, false
}

// unknownInputType sends an untested path to needs-review when it takes a
// type switch case that no test has ever taken, on a value whose origin
// PathKit cannot follow (so it cannot tell which types the value can hold).
type unknownInputType struct{}

func (unknownInputType) Name() string                             { return "unknown-input-type" }
func (unknownInputType) Labels() []string                         { return []string{"unknown-input-type"} }
func (unknownInputType) Detect(model.Path) (Classification, bool) { return Classification{}, false }

// engineClaim runs the fact engine on one path. It is only asked when no
// override, comment, rule or other detector said anything: those keep
// their label and wording.
func (e *Engine) engineClaim(workflow string, p model.Path) (Classification, bool) {
	an := e.facts[workflow]
	if an == nil || e.engineOff {
		return Classification{}, false
	}
	res := an.Analyze(p)
	if res.Claim != nil && e.detectorOn("value-flow") {
		c := DefaultTestable()
		label := string(res.Claim.Kind)
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = CannotBeTested, label, Certain, false
		c.Source, c.Detector, c.Explanation = SourceAuto, "value-flow", res.Claim.Explanation
		c.Proof = convertProof(res.Claim.Proof)
		c.Assumptions = res.Claim.Assumptions
		c.UsesStep = res.Claim.Uses
		return c, true
	}
	if res.Suspect != nil && e.detectorOn("possible-correlation") {
		c := DefaultTestable()
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "possible-correlation", Likely, true
		c.Source, c.Detector = SourceAuto, "possible-correlation"
		c.Explanation = res.Suspect.Explanation
		c.Proof = convertProof(res.Suspect.Proof)
		return c, true
	}
	if res.Unknown != nil && e.detectorOn("unknown-input-type") {
		c := DefaultTestable()
		c.Bucket, c.Label, c.Confidence, c.PossibleInProduction = NeedsReview, "unknown-input-type", Likely, true
		c.Source, c.Detector = SourceAuto, "unknown-input-type"
		c.Explanation = res.Unknown.Explanation
		c.Proof = convertProof(res.Unknown.Proof)
		c.SuspectExit, c.Group = res.Unknown.Exit, res.Unknown.Group
		return c, true
	}
	return Classification{}, false
}

func convertProof(in []facts.ProofStep) []ProofStep {
	out := make([]ProofStep, len(in))
	for i, s := range in {
		out[i] = ProofStep{Step: s.Step, File: s.File, Line: s.Line, Text: s.Text}
	}
	return out
}
