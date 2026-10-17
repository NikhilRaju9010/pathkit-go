package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// The assumption default-json-converter: on, claims rest on it and say so;
// off (by config or because the module sets its own converter), no claim
// about decoded values is made and every report says so.
func TestJSONConverterAssumption(t *testing.T) {
	const f, w = shapesDir + "/typeswitch", "typeswitch.SwitchOnDecodedInput"
	run := func(cfg scope.Classification, hits []string) ([]detectedPath, *Engine) {
		cfg.Enabled = true
		e, err := NewEngine(cfg)
		if err != nil {
			t.Fatal(err)
		}
		e.SetConverterScan(hits)
		g := graph(t, f, w)
		if err := e.AddGraph(w, g); err != nil {
			t.Fatal(err)
		}
		var out []detectedPath
		for _, p := range g.Paths(100).List {
			out = append(out, detectedPath{p, e.Classify(w, p)})
		}
		return out, e
	}
	finals := func(ds []detectedPath) []Classification {
		var out []Classification
		for _, d := range ds {
			out = append(out, d.C)
		}
		return out
	}

	ds, e := run(scope.Classification{JSONConverter: "auto"}, nil)
	cl := claimsOf(ds)
	c := cl["J1.case int|completed"]
	if c.Label != "impossible-type" || len(c.Assumptions) != 1 || c.Assumptions[0] != AssumptionJSON {
		t.Errorf("on: %+v", c)
	}
	notes := strings.Join(e.Notes(finals(ds), []string{w}), "\n")
	if !strings.Contains(notes, "Assumption default-json-converter: on; 1 claim rests on it") {
		t.Errorf("notes (on):\n%s", notes)
	}

	for name, tc := range map[string]struct {
		cfg  scope.Classification
		hits []string
		why  string
	}{
		"config off":    {scope.Classification{JSONConverter: "off"}, nil, "turned off in the config"},
		"custom module": {scope.Classification{JSONConverter: "auto"}, []string{"cmd/main.go:12: client.Options{DataConverter: c}"}, "cmd/main.go:12"},
	} {
		ds, e := run(tc.cfg, tc.hits)
		for key, c := range claimsOf(ds) {
			// without the assumption the origin is unknown: needs-review at most
			if c.Bucket == CannotBeTested {
				t.Errorf("%s: %s is claimed impossible: %+v", name, key, c)
			}
		}
		notes := strings.Join(e.Notes(finals(ds), []string{w}), "\n")
		if !strings.Contains(notes, "Assumption default-json-converter: OFF") || !strings.Contains(notes, tc.why) {
			t.Errorf("%s: notes:\n%s", name, notes)
		}
		if st := e.Assumptions(finals(ds)); len(st) != 1 || st[0].On || st[0].Why == "" {
			t.Errorf("%s: status %+v", name, st)
		}
	}
}

// Paths that take a type switch case on a value PathKit cannot follow are
// needs-review (unknown-input-type), and the notes group them by switch.
func TestUnknownInputTypeGroups(t *testing.T) {
	const w = "typeswitch.SwitchWithUnknownValue"
	ds := withEngine(t, scope.Classification{}, shapesDir+"/typeswitch", w)
	n := 0
	var finals []Classification
	for _, d := range ds {
		finals = append(finals, d.C)
		if d.C.Label == "unknown-input-type" {
			n++
			if d.C.Bucket != NeedsReview || d.C.Confidence != Likely || d.C.Group == "" || d.C.SuspectExit == "" || !strings.HasPrefix(d.C.SuspectExit, "J1.") {
				t.Errorf("suspect: %+v", d.C)
			}
		}
	}
	if n != 3 {
		t.Fatalf("%d unknown-input-type paths, want 3", n)
	}
	e, _ := NewEngine(scope.Classification{Enabled: true})
	notes := strings.Join(e.Notes(finals, []string{w}), "\n")
	if !strings.Contains(notes, "unknown-input-type: 3 paths take a type switch case") || !strings.Contains(notes, "typeswitch.go:") || !strings.Contains(notes, "(3 paths)") {
		t.Errorf("notes:\n%s", notes)
	}
	off := scope.Classification{DisabledDetectors: []scope.DisabledDetector{{Name: "unknown-input-type", Reason: "test"}}}
	for _, d := range withEngine(t, off, shapesDir+"/typeswitch", w) {
		if d.C.Label == "unknown-input-type" {
			t.Error("unknown-input-type must be switched off by name")
		}
	}
}
