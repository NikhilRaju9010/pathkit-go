package classify

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// A Detector looks at one path and, when the code makes the answer clear,
// says why a normal test cannot (or cannot easily) run it.
//
// Rules every detector follows (docs/DESIGN.md D13):
//   - It only reads the facts the extractor stuck on the path's junctions
//     (internal/model/facts.go). It never reads source text itself.
//   - It returns Certain only when it is sure. The engine turns anything
//     less (Likely) into needs-review, never into cannot-be-tested.
//   - It never looks at coverage. A path a test ran is put back to testable
//     by coverage.Compute, with a warning, whatever a detector said.
type Detector interface {
	// Name is the detector's name in the config (detectors.disable) and in
	// the "Detector ..." lines of the report. It is also its label.
	Name() string
	// Labels are the labels this detector can put on a path.
	Labels() []string
	Detect(p model.Path) (Classification, bool)
}

// detectors lists every detector, in the order they are tried. The first
// one that applies is the main reason; the others are kept as extra
// reasons. Add new detectors here, one line each.
var detectors = []Detector{
	closedSignalChannel{},
	constantCondition{},
	versionGateOldBranch{},
	historySuggested{},
	contradictory{},
	sameListLoops{},
	listBuiltFromList{},
	loopExitBeforeFirstTrip{},
	contradictoryGetResult{},
	valueFlow{},
	possibleCorrelation{},
	unknownInputType{},
	likelyDefect{},
}

// DetectorNames lists the names of all detectors.
func DetectorNames() []string {
	var out []string
	for _, d := range detectors {
		out = append(out, d.Name())
	}
	return out
}

// setDetectors turns on every detector except those the config disabled.
func (e *Engine) setDetectors(disabled []scope.DisabledDetector) error {
	if err := CheckDetectorNames(disabled); err != nil {
		return err
	}
	off := map[string]bool{}
	for _, d := range disabled {
		off[d.Name] = true
	}
	e.disabled = disabled
	for _, det := range detectors {
		if !off[det.Name()] {
			e.detectors = append(e.detectors, det)
		}
	}
	return nil
}

// settle applies the one rule that is the same for every detector: only a
// certain decision may keep a bucket other than needs-review.
func (e *Engine) settle(c Classification) Classification {
	c.Source = SourceAuto
	if c.Confidence != Certain {
		c.Bucket = NeedsReview
		c.PossibleInProduction = true
	}
	return c
}

// detectorOn reports whether the named detector is on (not turned off in
// the config).
func (e *Engine) detectorOn(name string) bool {
	for _, d := range e.disabled {
		if d.Name == name {
			return false
		}
	}
	return true
}

// CheckDetectorNames refuses a detectors.disable entry that is not a real
// detector. It does not depend on classification being enabled: a typo is
// reported whenever the config is loaded, so it cannot sit unnoticed until
// someone switches classification on.
func CheckDetectorNames(disabled []scope.DisabledDetector) error {
	for _, d := range disabled {
		known := false
		for _, det := range detectors {
			known = known || det.Name() == d.Name
		}
		if !known {
			return fmt.Errorf("classification.detectors.disable: %q is not a detector (detectors: %s)", d.Name, strings.Join(DetectorNames(), ", "))
		}
	}
	return nil
}
