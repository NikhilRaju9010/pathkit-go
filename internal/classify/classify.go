// Package classify puts every listed path in a bucket (testable,
// cannot-be-tested, needs-special-test, needs-review) with a reason label,
// and holds the one list that defines every bucket, label and comment
// PathKit can print (definitions.go).
//
// Classify, the classifier used when no config is given, puts every path
// in the testable bucket. That is the safe direction: an
// unclassified path is never hidden, it only counts against coverage.
package classify

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Bucket says whether a normal test can run a path.
type Bucket string

// The four buckets. A path a test ran is always Testable.
const (
	Testable         Bucket = "testable"
	CannotBeTested   Bucket = "cannot-be-tested"
	NeedsSpecialTest Bucket = "needs-special-test"
	NeedsReview      Bucket = "needs-review"
)

// Buckets lists every bucket in report order.
var Buckets = []Bucket{Testable, CannotBeTested, NeedsSpecialTest, NeedsReview}

// Source says who decided a path's bucket.
type Source string

const (
	SourceAuto   Source = "auto"   // a detector
	SourceManual Source = "manual" // a person, in the config file or a comment
)

// Confidence says how sure an automatic decision is. Only "certain" may
// move a path to cannot-be-tested; "likely" goes to needs-review.
type Confidence string

// Certain and Likely are the two confidence levels of a detector.
const (
	Certain Confidence = "certain"
	Likely  Confidence = "likely"
)

// Classification is everything PathKit says about one path's bucket.
type Classification struct {
	Bucket Bucket
	// Label is the reason label ("closed-signal-channel", ...), "" for a
	// plain testable path. Every non-empty Label has a Definition.
	Label                string
	Source               Source
	Confidence           Confidence
	PossibleInProduction bool
	Explanation          string
	// Origin says which written-down source decided it ("" for the default
	// or a detector). RuleID is the config rule that decided it, "" for none.
	Origin Origin
	RuleID string
	// Detector names the detector that decided it, "" otherwise.
	Detector string
	// ExtraReasons lists other things that also applied (a rule behind an
	// override, a second rule): the main reason is the first that applies,
	// the rest are shown for transparency. Empty for most paths.
	ExtraReasons []string
	// Proof is the reasoning behind a detector's claim, step by step in
	// plain words. Empty for a plain testable path and for a claim a
	// person made (config override, comment, rule).
	Proof []ProofStep
	// Assumptions are the named assumptions the claim rests on
	// ("default-json-converter"); empty for most claims.
	Assumptions []string
	// SuspectExit and Group are set for unknown-input-type: the edge id of
	// the type switch case the path takes, and the switch ("file:line").
	SuspectExit string
	Group       string
	// UsesStep is the last step of the path (1-based) the claim's reasoning
	// depends on; 0 when it is not known. Local distrust of a trace that
	// leaves the map uses it (DowngradeUnexplained).
	UsesStep int
}

// Origin is the written-down source of a classification.
type Origin string

const (
	OriginOverride Origin = "config override" // mark_testable / mark_untestable
	OriginComment  Origin = "comment"         // //pathkit:untestable
	OriginRule     Origin = "rule"            // a config rule
	// OriginUnexplained: a rule or detector said cannot-be-tested, but the
	// workflow has a trace that fits no path, so the claim was not trusted
	// (see DowngradeUnexplained).
	OriginUnexplained Origin = "unexplained trace"
)

// DowngradeUnexplained is what happens to a cannot-be-tested claim made by
// a detector or a config rule in a workflow that has a trace fitting no
// path: the map of that workflow is known to be incomplete, so the claim
// is not trusted. The path becomes needs-review (label model-limit), which
// under the default policy counts as testable, so the number cannot go up
// because of it. A person's own decision (a config override or a
// //pathkit:untestable comment) is kept: changed is false.
//
// why is the sentence that names the trace(s).
func DowngradeUnexplained(c Classification, why string) (out Classification, changed bool) {
	if c.Bucket != CannotBeTested || c.Origin == OriginOverride || c.Origin == OriginComment {
		return c, false
	}
	was := strings.TrimSuffix(c.Explanation, ".")
	by := "a detector"
	switch {
	case c.Origin == OriginRule:
		by = fmt.Sprintf("rule %q", c.RuleID)
	case c.Detector != "":
		by = "detector " + c.Detector
	}
	out = c
	out.Bucket, out.Label, out.Confidence, out.PossibleInProduction = NeedsReview, "model-limit", Likely, true
	out.Origin = OriginUnexplained
	out.Proof = nil // the proof belonged to the claim that is no longer trusted
	out.Explanation = fmt.Sprintf("%s would mark this path cannot-be-tested (%s), but %s; the claim is not trusted until that trace is explained.", by, was, why)
	return out, true
}

// Classifier decides one path's classification. workflow is the full
// workflow name ("orders.OrderWorkflow"): a stableId is only unique within
// one workflow.
type Classifier func(workflow string, p model.Path) Classification

// Classify is the classifier PathKit uses when the config gives none:
// with no detectors, rules or overrides, every path is testable.
func Classify(string, model.Path) Classification { return DefaultTestable() }

// DefaultTestable is the classification of a path nothing says is
// impossible: the default (docs/DESIGN.md D13).
func DefaultTestable() Classification {
	return Classification{
		Bucket: Testable, Source: SourceAuto, Confidence: Certain, PossibleInProduction: true,
		Explanation: "No detector or rule says this path is impossible, so a normal test is expected to be able to run it.",
	}
}

// Policy says how needs-review paths count in testable coverage
// (unclassified_policy in the config).
type Policy string

const (
	// CountAsTestable (the default, strict): needs-review paths count in
	// testable coverage, so they can only lower the number.
	CountAsTestable Policy = "count_as_testable"
	// Exclude: needs-review paths are left out of testable coverage but
	// still listed.
	Exclude Policy = "exclude"
)
