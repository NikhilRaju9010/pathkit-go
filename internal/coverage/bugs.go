package coverage

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
)

// PossibleBug is a place where the code tests a pointed-to type or value on a
// pointer (label likely-defect), found by the fact engine.
type PossibleBug struct {
	Workflow string
	File     string
	Line     int
	// Hint is the sentence of the label's proof ("This value is a pointer ...").
	Hint string
	// Paths is how many listed paths take it.
	Paths int
}

// PossibleBugs lists each place once, in workflow order. The place is the
// decision the proof names; a path with no proof position is placed at the
// workflow's file.
func PossibleBugs(res Result) []PossibleBug {
	var out []PossibleBug
	at := map[string]int{}
	for _, w := range res.Workflows {
		for _, p := range w.Paths {
			if p.Class.Label != "likely-defect" || p.Class.Bucket != classify.NeedsReview {
				continue
			}
			file, line := w.File, 0
			for _, s := range p.Class.Proof {
				if s.Line > 0 && s.Step > 0 {
					file, line = s.File, s.Line
				}
			}
			hint := ""
			if n := len(p.Class.Proof); n > 0 {
				hint = p.Class.Proof[n-1].Text
			}
			key := fmt.Sprintf("%s|%s|%d", w.Name, file, line)
			if i, ok := at[key]; ok {
				out[i].Paths++
				continue
			}
			at[key] = len(out)
			out = append(out, PossibleBug{Workflow: w.Name, File: file, Line: line, Hint: hint, Paths: 1})
		}
	}
	return out
}
