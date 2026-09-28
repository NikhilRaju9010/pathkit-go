package model_test

import (
	"errors"
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

// mappedWorkflows are compared path by path. The rest use constructs
// planned for a later M4 slice and must be skipped, naming that construct.
var mappedWorkflows = []string{
	"orders.OrderWorkflow", "fulfillment.PaymentWorkflow", "reports.DailyReportWorkflow", // M2
	"polling.ReportPollingWorkflow", "billing.SubscriptionWorkflow", // M4b
}

var m4Workflows = map[string]string{
	"approval.ApprovalWorkflow":            "result of AwaitWithTimeout used in an if",
	"shipment.ShipmentWorkflow":            "workflow.Selector",
	"fulfillment.OrderFulfillmentWorkflow": "defer with a Temporal call (saga compensation)",
}

func TestPilotMatchesExpected(t *testing.T) {
	expected, err := expectedkey.Read(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	workflows := workflowsIn(t, "../../testdata/pilot/...")

	if len(expected) != 8 {
		t.Fatalf("EXPECTED.md: parsed %d workflow sections, want 8", len(expected))
	}
	for name, ew := range expected {
		if len(ew.Paths) != ew.Count {
			t.Errorf("EXPECTED.md, %s: says %d paths but lists %d", name, ew.Count, len(ew.Paths))
		}
	}

	for _, name := range mappedWorkflows {
		t.Run(name, func(t *testing.T) {
			wf, ok := workflows[name]
			if !ok {
				t.Fatalf("%s not found by discovery", name)
			}
			g, err := model.Build(wf)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			got := pathKeys(g)
			var want []string
			for _, p := range expected[name].Paths {
				// The note is part of what must match: a note in the key
				// that the model doesn't produce is a disagreement.
				k := p.Key
				if p.Compensation {
					k += " [compensation (defer)]"
				}
				want = append(want, k)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("analyzer and EXPECTED.md disagree for %s\n analyzer (%d paths):\n   %s\n EXPECTED.md (%d paths):\n   %s",
					name, len(got), strings.Join(got, "\n   "), len(want), strings.Join(want, "\n   "))
			}
			t.Logf("%s: %d paths match EXPECTED.md", name, len(got))
		})
	}

	for name, construct := range m4Workflows {
		t.Run(name, func(t *testing.T) {
			wf, ok := workflows[name]
			if !ok {
				t.Fatalf("%s not found by discovery", name)
			}
			_, err := model.Build(wf)
			var u *model.UnsupportedError
			if !errors.As(err, &u) || u.Construct != construct {
				t.Fatalf("want skipped for %q (planned for a later M4 slice), got %v", construct, err)
			}
			t.Logf("%s: skipped until M4 (%v)", name, err)
		})
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
