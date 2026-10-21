package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The default report is made for people who test: branch coverage first, the
// workflows worst first, what to test next, and the older report under "More
// detail". With --summary there is no detail. Nothing in it is technical-only
// jargon: the technical names live in --technical.
func TestReportForPeople(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, stderr, code := runDefault(t, "report", "./orders", "--traces", traces)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(out, "Branch coverage: 3 / 4 outcomes that can happen — 75.0% — commendable\n") {
		t.Errorf("the first line should be the branch coverage headline:\n%s", firstLines(out, 8))
	}
	for _, want := range []string{"Two-step combinations:", "Blocked by known bugs: 0", "Tests cover 3 of 4 decision outcomes that can happen", "What to test next", "ChargeCard never succeeded here", "Test name: Test", "1 of 1 workflows at commendable or better", "More detail"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if i := strings.Index(out, "More detail"); i > 0 {
		for _, jargon := range []string{"cannot-be-tested", "needs-review", "decision pairs"} {
			if strings.Contains(out[i:], jargon) {
				t.Errorf("the detail should use plain names, found %q", jargon)
			}
		}
	}
	sum, _, _ := runDefault(t, "report", "./orders", "--traces", traces, "--summary")
	if strings.Contains(sum, "More detail") || !strings.Contains(sum, "What to test next") {
		t.Errorf("--summary has no detail but has the next list:\n%s", sum)
	}
	// near miss: --technical is the older text, without the new headline
	tech, _, _ := runDefault(t, "report", "./orders", "--traces", traces, "--technical")
	if strings.Contains(tech, "Branch coverage:") || strings.Contains(tech, "What to test next") || !strings.Contains(tech, "paths total") {
		t.Errorf("--technical must print the older report:\n%s", firstLines(tech, 6))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func TestCoverageDefaultStartsWithBranches(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, _, code := runDefault(t, "coverage", "./orders", "--traces", traces)
	if code != ExitOK || !strings.HasPrefix(out, "Branch coverage: 3 / 4 outcomes") {
		t.Errorf("coverage should lead with branch coverage (exit %d):\n%s", code, firstLines(out, 5))
	}
	tech, _, _ := runDefault(t, "coverage", "./orders", "--traces", traces, "--technical")
	if strings.Contains(tech, "Branch coverage:") {
		t.Errorf("--technical coverage is the older text")
	}
}

func TestBranchesCommand(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, stderr, code := runDefault(t, "branches", "./orders", "--traces", traces)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit %d %q", code, stderr)
	}
	for _, want := range []string{"Branch coverage: 3 / 4", "orders.OrderWorkflow", "Next: pathkit next"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "What to test next") || strings.Contains(out, "Missed outcomes") {
		t.Errorf("branches prints the headline and the table only unless --missed:\n%s", out)
	}
	m, _, _ := runDefault(t, "branches", "./orders", "--traces", traces, "--missed")
	if !strings.Contains(m, "Missed outcomes") || !strings.Contains(m, "ChargeCard never succeeded here") {
		t.Errorf("--missed lists the missed outcomes:\n%s", m)
	}
	js, _, _ := runDefault(t, "branches", "./orders", "--traces", traces, "--json")
	var d struct {
		BranchCoverage struct {
			Covered, Possible int
			Grade             string
		}
		Workflows []struct{ Name string }
		Missed    []struct{ Sentence string }
	}
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		t.Fatal(err)
	}
	if d.BranchCoverage.Covered != 3 || d.BranchCoverage.Possible != 4 || len(d.Workflows) != 1 || len(d.Missed) != 1 {
		t.Errorf("json: %+v", d)
	}
	tech, _, _ := runDefault(t, "branches", "./orders", "--traces", traces, "--technical")
	if !strings.Contains(tech, "Branches (exits taken): 3/4") {
		t.Errorf("--technical uses the technical names:\n%s", tech)
	}
}

func TestNextCommand(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, _, code := runDefault(t, "next", "./orders", "--traces", traces)
	if code != ExitOK || !strings.Contains(out, "1. [") || !strings.Contains(out, "ChargeCard never succeeded here") || !strings.Contains(out, "Next: pathkit next --scaffold") {
		t.Errorf("next:\n%s", out)
	}
	sc, _, code := runDefault(t, "next", "./orders", "--traces", traces, "--scaffold")
	if code != ExitOK || !strings.Contains(sc, "func Test") || !strings.Contains(sc, "testsuite") {
		t.Errorf("--scaffold should print a Go test:\n%s", sc)
	}
	js, _, _ := runDefault(t, "next", "./orders", "--traces", traces, "--json", "--top", "1")
	if !strings.Contains(js, `"next"`) || !strings.Contains(js, `"sentence"`) {
		t.Errorf("json: %s", js)
	}
}
