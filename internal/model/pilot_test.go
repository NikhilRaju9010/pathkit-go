package model_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	expectedkey "github.com/NikhilRaju9010/pathkit-go/internal/expected"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The answer key, testdata/pilot/EXPECTED.md, was written by hand in M1
// before PathKit could analyze anything. This test reads it as-is.
// RULE: never edit EXPECTED.md to make this test pass. If the analyzer and
// the key disagree and the key looks wrong, stop and ask the project owner.

const expectedFile = "../../testdata/pilot/EXPECTED.md"

// pilotKey reads the answer key. Every list of pilot workflows and tests
// comes from it, never from a hand-written list.
func pilotKey(t *testing.T) map[string]*expectedkey.Workflow {
	t.Helper()
	key, err := expectedkey.Read(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// keyWorkflows lists the answer key's workflows, sorted.
func keyWorkflows(t *testing.T) []string {
	t.Helper()
	return expectedkey.Names(pilotKey(t))
}

// withNote is a path's key plus its compensation note, when it has one:
// the note is part of what must match.
func withNote(key string, compensation bool) string {
	if compensation {
		return key + " " + model.CompensationNote
	}
	return key
}

// Every workflow in EXPECTED.md must be mapped and match the key path for
// path (steps, end kind and note). A workflow that is skipped, or that
// discovery doesn't find, FAILS this test (expectedkey.Build).
func TestPilotMatchesExpected(t *testing.T) {
	key := pilotKey(t)
	workflows := workflowsIn(t, "../../testdata/pilot/...")

	names := expectedkey.Names(key)
	var found []string
	for name := range workflows {
		found = append(found, name)
	}
	slices.Sort(found)
	if !slices.Equal(names, found) {
		t.Errorf("EXPECTED.md lists %v, but discovery found %v", names, found)
	}

	paths := 0
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			g, err := expectedkey.Build(workflows, name)
			if err != nil {
				t.Fatal(err)
			}
			var got, want []string
			for _, p := range g.Paths(model.DefaultMaxPaths).List {
				got = append(got, withNote(p.Key(), p.Compensation))
			}
			for _, p := range key[name].Paths {
				want = append(want, withNote(p.Key, p.Compensation))
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("analyzer and EXPECTED.md disagree for %s\n analyzer (%d paths):\n   %s\n EXPECTED.md (%d paths):\n   %s",
					name, len(got), strings.Join(got, "\n   "), len(want), strings.Join(want, "\n   "))
				return
			}
			paths += len(got)
			t.Logf("%s: %d paths match EXPECTED.md", name, len(got))
		})
	}
	if paths != 38 {
		t.Errorf("%d paths matched EXPECTED.md, want all 38", paths)
	}
}

// The child workflow step is labelled with the child's name (M4d: child
// label end to end; the match above checks its exits).
func TestPilotChildWorkflowLabel(t *testing.T) {
	g := pilotGraph(t, "fulfillment.OrderFulfillmentWorkflow")
	if got := g.Junctions[1].Label; got != "PaymentWorkflow (child workflow)" {
		t.Errorf("fulfillment J2 label = %q, want %q", got, "PaymentWorkflow (child workflow)")
	}
}

// The loop rule was widened on 2026-09-28: a loop is a junction if it
// contains a Temporal call (the original D3 rule) or any junction (new).
// This pins that the change can't move any EXPECTED.md path: every loop
// in every pilot workflow contains a Temporal call, so it was a junction
// under the old rule and still is under the new one. (And the path lists
// above are compared with EXPECTED.md as they are.)
func TestPilotLoopRuleChangeIsNeutral(t *testing.T) {
	workflows := workflowsIn(t, "../../testdata/pilot/...")
	if len(workflows) != 8 {
		t.Fatalf("found %d pilot workflows, want 8", len(workflows))
	}
	var seen []string
	for name, wf := range workflows {
		for _, l := range model.LoopReasons(wf) {
			where := fmt.Sprintf("%s (%s:%d)", name, filepath.Base(l.Pos.Filename), l.Pos.Line)
			seen = append(seen, where)
			if !l.Temporal {
				t.Errorf("%s: loop without a Temporal call; the old and new loop rules could disagree here", where)
			}
		}
	}
	slices.Sort(seen)
	want := []string{"billing.SubscriptionWorkflow (subscription.go:20)", "polling.ReportPollingWorkflow (polling.go:21)"}
	if !slices.Equal(seen, want) {
		t.Errorf("pilot loops = %v, want exactly %v", seen, want)
	}
}
