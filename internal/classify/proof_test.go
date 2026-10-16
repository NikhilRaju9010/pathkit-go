package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// Every claim a detector makes carries a proof: plain sentences, at least
// one tied to a real step of the path and a line of the code, and the last
// one the conclusion. A plain testable path has none.
func TestEveryDetectorClaimHasAProof(t *testing.T) {
	e, err := NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{} // detector -> claims checked
	for _, dir := range []string{"closedchannel", "constcond", "dataflow", "dataflowget", "loopfirst", "loopfirstnamed", "builtlist", "versiongate", "historysuggest", "loops", "waits"} {
		res, err := load.Load("../../testdata/fixtures/" + dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			if err != nil {
				continue // some fixtures hold workflows PathKit refuses on purpose
			}
			for _, p := range g.Paths(model.DefaultMaxPaths).List {
				c := e.Claim(wf.Name, p)
				if c.Detector == "" {
					if len(c.Proof) != 0 {
						t.Errorf("%s %s: a path no detector claimed has a proof", wf.Name, p.Key())
					}
					continue
				}
				seen[c.Detector]++
				if len(c.Proof) < 2 {
					t.Errorf("%s %s (%s): proof has %d steps, want at least 2: %+v", wf.Name, p.Key(), c.Detector, len(c.Proof), c.Proof)
					continue
				}
				positional := 0
				for i, s := range c.Proof {
					if strings.TrimSpace(s.Text) == "" {
						t.Errorf("%s %s: proof step %d has no text", wf.Name, p.Key(), i+1)
					}
					if s.Step > 0 {
						positional++
						if s.Step > len(p.Steps) || s.Line <= 0 || s.File == "" {
							t.Errorf("%s %s: proof step %d points at step %d, %s:%d (path has %d steps)", wf.Name, p.Key(), i+1, s.Step, s.File, s.Line, len(p.Steps))
						}
					}
				}
				if positional == 0 {
					t.Errorf("%s %s: no proof step points at a step of the path", wf.Name, p.Key())
				}
				if last := c.Proof[len(c.Proof)-1]; last.Step != 0 {
					t.Errorf("%s %s: the last proof step should be the conclusion (step 0), got step %d", wf.Name, p.Key(), last.Step)
				}
			}
		}
	}
	for _, d := range []string{"closed-signal-channel", "constant-condition", "contradictory", "contradictory-get-result", "loop-exit-before-first-trip", "same-list-loops", "list-built-from-list", "version-gate-old-branch", "history-limit"} {
		if seen[d] == 0 {
			t.Errorf("no fixture path was claimed by detector %s, so its proof was not checked", d)
		}
	}
}

// A claim that is no longer trusted (unexplained trace) drops its proof:
// the proof belonged to the claim.
func TestDowngradeDropsTheProof(t *testing.T) {
	c := DefaultTestable()
	c.Bucket, c.Label, c.Detector, c.Explanation = CannotBeTested, "contradictory", "contradictory", "x"
	c.Proof = []ProofStep{{Text: "a"}, {Text: "b"}}
	out, changed := DowngradeUnexplained(c, "a trace fits no path")
	if !changed || out.Proof != nil {
		t.Errorf("changed=%v proof=%v", changed, out.Proof)
	}
}
