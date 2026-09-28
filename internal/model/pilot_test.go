package model_test

import (
	"errors"
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

// m2Workflows are compared path by path. The rest use constructs planned
// for M4 and must be skipped, naming that construct.
var m2Workflows = []string{"orders.OrderWorkflow", "fulfillment.PaymentWorkflow", "reports.DailyReportWorkflow"}

var m4Workflows = map[string]string{
	"approval.ApprovalWorkflow":            "result of AwaitWithTimeout used in an if",
	"polling.ReportPollingWorkflow":        "for loop",
	"shipment.ShipmentWorkflow":            "workflow.Selector",
	"fulfillment.OrderFulfillmentWorkflow": "defer with a Temporal call (saga compensation)",
	"billing.SubscriptionWorkflow":         "for loop",
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

	for _, name := range m2Workflows {
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
			want := slices.Clone(expected[name].Paths)
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
				t.Fatalf("want skipped for %q (planned for M4), got %v", construct, err)
			}
			t.Logf("%s: skipped until M4 (%v)", name, err)
		})
	}
}
