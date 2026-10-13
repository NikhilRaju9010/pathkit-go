package classify

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// detected classifies every path of one fixture workflow with a fresh
// engine (classification on, no overrides, comments or rules) and returns
// each path with its classification.
type detectedPath struct {
	Path model.Path
	C    Classification
}

func detected(t *testing.T, folder, name string, disabled ...scope.DisabledDetector) []detectedPath {
	t.Helper()
	e, err := NewEngine(scope.Classification{Enabled: true, DisabledDetectors: disabled})
	if err != nil {
		t.Fatal(err)
	}
	var out []detectedPath
	for _, p := range graph(t, folder, name).Paths(model.DefaultMaxPaths).List {
		out = append(out, detectedPath{p, e.Classify(name, p)})
	}
	return out
}

// takes reports whether the path goes through the exit with this ID.
func takes(p model.Path, edge string) bool {
	for _, s := range p.Steps {
		if s.Exit.ID.String() == edge {
			return true
		}
	}
	return false
}

// expectation: every path that takes `edge` has this bucket and label; every
// other path is testable. An empty edge means every path is testable.
type expectation struct {
	workflow string
	edge     string
	bucket   Bucket
	label    string
}

func checkExpectations(t *testing.T, folder, prefix string, cases []expectation) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.workflow, func(t *testing.T) {
			paths := detected(t, folder, prefix+c.workflow)
			hit := 0
			for _, d := range paths {
				if c.edge != "" && takes(d.Path, c.edge) {
					hit++
					if d.C.Bucket != c.bucket || d.C.Label != c.label {
						t.Errorf("path %s: %s/%q, want %s/%q (%s)", d.Path.Key(), d.C.Bucket, d.C.Label, c.bucket, c.label, d.C.Explanation)
					}
					if d.C.Source != SourceAuto || d.C.Explanation == "" {
						t.Errorf("path %s: a detector's answer needs source auto and an explanation: %+v", d.Path.Key(), d.C)
					}
					continue
				}
				if d.C.Bucket != Testable || d.C.Label != "" {
					t.Errorf("path %s: %s/%q, want testable (%s)", d.Path.Key(), d.C.Bucket, d.C.Label, d.C.Explanation)
				}
			}
			if c.edge != "" && hit == 0 {
				t.Errorf("no path takes %s: the fixture or the test is wrong", c.edge)
			}
		})
	}
}

// Detectors and definitions cannot drift apart: a label decided by a
// detector either has a detector that can produce it, or says it is not
// detected yet; and a detector never produces a label that says "not yet".
func TestDetectorsMatchDefinitions(t *testing.T) {
	produced := map[string]string{}
	for _, d := range detectors {
		for _, l := range d.Labels() {
			if _, ok := Lookup(KindLabel, l); !ok {
				t.Errorf("detector %s uses label %q, which has no definition", d.Name(), l)
			}
			produced[l] = d.Name()
		}
	}
	for _, def := range Definitions() {
		if def.Kind != KindLabel || def.DecidedBy != "detector" {
			continue
		}
		by, has := produced[def.Name]
		switch {
		case has && def.NotDetectedYet:
			t.Errorf("label %q is marked 'not detected yet' but detector %s produces it", def.Name, by)
		case !has && !def.NotDetectedYet:
			t.Errorf("label %q is decided by a detector, but none produces it and it is not marked 'not detected yet'", def.Name)
		}
	}
	names := map[string]bool{}
	for _, d := range detectors {
		if names[d.Name()] {
			t.Errorf("two detectors are called %q", d.Name())
		}
		names[d.Name()] = true
	}
}

// With classification on and every detector running, the pilot (which has
// no closed channel, constant, GetVersion or history check) stays all
// testable: no detector may fire on it.
func TestPilotStaysAllTestableWithDetectors(t *testing.T) {
	e, err := NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := load.Load(pilot)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.Build(wf)
		if err != nil {
			t.Fatalf("%s: %v", wf.Name, err)
		}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			n++
			if c := e.Classify(wf.Name, p); c.Bucket != Testable || c.Label != "" {
				t.Errorf("%s %s: %s/%s (%s)", wf.Name, p.StableID(), c.Bucket, c.Label, c.Explanation)
			}
		}
	}
	if n != 38 {
		t.Errorf("checked %d pilot paths, want 38", n)
	}
}
