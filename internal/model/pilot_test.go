package model_test

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

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

type expectedWorkflow struct {
	count int      // the "**K paths:**" number
	paths []string // keys like "J1.false J2.success|completed"
}

var (
	sectionRe = regexp.MustCompile("^## \\d+\\. `([\\w.]+)`")
	countRe   = regexp.MustCompile(`^\*\*(\d+) paths:\*\*`)
	pathRe    = regexp.MustCompile(`^(\d+)\. (.*)$`)
	stepRe    = regexp.MustCompile(`J(\d+)\b.*?--(.+?)-->`)
	endRe     = regexp.MustCompile(`End \(([a-z-]+)\)`)
)

// parseExpected reads every workflow section's path list from EXPECTED.md.
func parseExpected(t *testing.T) map[string]expectedWorkflow {
	t.Helper()
	f, err := os.Open(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	out := map[string]expectedWorkflow{}
	var current string
	inPaths := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			current, inPaths = m[1], false
			continue
		}
		if current == "" {
			continue
		}
		if m := countRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			out[current] = expectedWorkflow{count: n}
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		m := pathRe.FindStringSubmatch(line)
		if m == nil {
			if strings.TrimSpace(line) != "" {
				inPaths = false // the path list has ended
			}
			continue
		}
		ew := out[current]
		ew.paths = append(ew.paths, keyFromLine(t, current, m[2]))
		out[current] = ew
	}
	return out
}

// keyFromLine turns "J1 --false--> J2 ChargeCard --success--> End (completed)"
// into "J1.false J2.success|completed".
func keyFromLine(t *testing.T, workflow, line string) string {
	t.Helper()
	var steps []string
	for _, m := range stepRe.FindAllStringSubmatch(line, -1) {
		steps = append(steps, "J"+m[1]+"."+m[2])
	}
	end := endRe.FindStringSubmatch(line)
	if end == nil {
		t.Fatalf("EXPECTED.md, %s: no end kind in %q", workflow, line)
	}
	return strings.Join(steps, " ") + "|" + end[1]
}

func TestPilotMatchesExpected(t *testing.T) {
	expected := parseExpected(t)
	workflows := workflowsIn(t, "../../testdata/pilot/...")

	if len(expected) != 8 {
		t.Fatalf("EXPECTED.md: parsed %d workflow sections, want 8", len(expected))
	}
	for name, ew := range expected {
		if len(ew.paths) != ew.count {
			t.Errorf("EXPECTED.md, %s: says %d paths but lists %d", name, ew.count, len(ew.paths))
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
			want := slices.Clone(expected[name].paths)
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
