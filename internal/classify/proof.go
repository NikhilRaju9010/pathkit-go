package classify

import (
	"fmt"
	"path/filepath"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// ProofStep is one step of the reasoning behind a claim, in plain words.
// Step is the 1-based number of the step on the path that the sentence is
// about, or 0 for the closing "so ..." sentence that is not about one step.
// File (base name) and Line say where in the code that step is; both are
// empty/0 when Step is 0.
type ProofStep struct {
	Step int
	File string
	Line int
	Text string
}

// stepProof makes the proof step for the first time path p passes junction
// j. A detector only passes junctions it found on the path; if j is not
// there anyway, the sentence is kept with Step 0 and no position.
func stepProof(p model.Path, j *model.Junction, text string) ProofStep {
	for i, s := range p.Steps {
		if s.Junction == j {
			if j.Pos.Filename == "" {
				return ProofStep{Step: i + 1, Text: text}
			}
			return ProofStep{Step: i + 1, File: filepath.Base(j.Pos.Filename), Line: j.Pos.Line, Text: text}
		}
	}
	return ProofStep{Text: text}
}

// conclusion is the last proof step: what the steps before it add up to.
func conclusion(text string) ProofStep { return ProofStep{Text: text} }

// where names a junction in a proof sentence: "J3 (if x > 0)".
func where(j *model.Junction) string { return fmt.Sprintf("%s (%s)", j.ID, j.Label) }
