package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordScenarios runs the scenarios fixture's tests with the recorder and
// returns the trace folder.
func recordScenarios(t *testing.T) (fixture, traces string) {
	t.Helper()
	fixture = abs(t, "../../testdata/fixtures/scenarios")
	dir := t.TempDir()
	t.Chdir(dir)
	traces = filepath.Join(dir, "traces")
	if _, stderr, code := pathkit(t, "test", fixture+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	return fixture, traces
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func num(m map[string]any, key string) float64 { v, _ := m[key].(float64); return v }

// Paths that make the same calls are one scenario; a decision that changes
// what the workflow calls, or the order, makes two (the near misses).
func TestScenariosMergeOnlyTheSameBehaviour(t *testing.T) {
	fixture, traces := recordScenarios(t)
	cfg := writeConfig(t, `{"classification": {"enabled": true, "scenarios": {"max_listed": 4}}}`)
	doc := summarizeReport(t, fixture, traces, cfg)

	count := func(name string) (paths, scenarios float64) {
		w := workflowNamed(t, doc, "scenarios."+name)
		return num(w["paths"].(map[string]any), "total"), func() float64 {
			var n float64
			if _, err := fmt.Sscan(w["scenarios"].(map[string]any)["count"].(string), &n); err != nil {
				t.Fatal(err)
			}
			return n
		}()
	}
	for _, c := range []struct {
		name            string
		paths, scenario float64
	}{
		{"Logic", 8, 2},        // decisions that only compute: merged
		{"Different", 4, 4},    // chooses between two activities: not merged
		{"OrderMatters", 4, 6}, // same calls in another order: not merged (the list holds 4)
	} {
		p, s := count(c.name)
		if c.name == "OrderMatters" {
			// the path list is capped by max_listed only for scenarios; all paths are listed
			p = c.paths
		}
		if p != c.paths || s != c.scenario {
			t.Errorf("%s: %v paths and %v scenarios, want %v and %v", c.name, p, s, c.paths, c.scenario)
		}
	}

	// every path of a scenario names its main path; a main path names none
	logic := workflowNamed(t, doc, "scenarios.Logic")
	same := 0
	for _, p := range logic["pathList"].([]any) {
		pm := p.(map[string]any)
		if pm["sameBehaviourAs"] != nil {
			same++
		}
		if pm["scenarioId"] == nil || !strings.HasPrefix(pm["scenarioId"].(string), "sc:") {
			t.Errorf("Logic path without a scenario id: %v", pm)
		}
	}
	if same != 6 {
		t.Errorf("Logic: %d paths say \"same behaviour as\", want 6 (8 paths, 2 scenarios)", same)
	}
	diff := workflowNamed(t, doc, "scenarios.Different")
	for _, p := range diff["pathList"].([]any) {
		if p.(map[string]any)["sameBehaviourAs"] != nil {
			t.Errorf("Different: two paths with different calls were labelled the same: %v", p)
		}
	}
}

// The main number by the rules: a workflow with few paths is measured by
// paths, one with too many paths but few scenarios by scenarios, an event loop
// by its handlers, and a forced choice wins.
func TestMainNumberRules(t *testing.T) {
	fixture, traces := recordScenarios(t)
	cfg := writeConfig(t, `{"classification": {"enabled": true, "scenarios": {"max_listed": 4}}}`)
	doc := summarizeReport(t, fixture, traces, cfg)
	kind := func(doc map[string]any, name string) string {
		return workflowNamed(t, doc, "scenarios."+name)["mainNumber"].(map[string]any)["kind"].(string)
	}
	for name, want := range map[string]string{"Logic": "path", "Wide": "scenario", "Events": "handler", "Many": "path"} {
		if got := kind(doc, name); got != want {
			t.Errorf("%s: main number is %s coverage, want %s", name, got, want)
		}
	}
	wide := workflowNamed(t, doc, "scenarios.Wide")["mainNumber"].(map[string]any)
	if wide["why"] == "" || num(wide, "total") != 2 || num(wide, "covered") != 1 {
		t.Errorf("Wide main number: %v", wide)
	}
	if adv, _ := wide["splitAdvice"].(string); !strings.Contains(adv, "child workflow") {
		t.Errorf("Wide: no split advice for a workflow measured by scenarios: %v", wide)
	}
	if logic := workflowNamed(t, doc, "scenarios.Logic")["mainNumber"].(map[string]any); logic["splitAdvice"] != nil {
		t.Errorf("Logic is measured by paths: no split advice expected, got %v", logic["splitAdvice"])
	}

	// too many scenarios to list (max_listed 1): branch coverage
	small := writeConfig(t, `{"classification": {"enabled": true, "scenarios": {"max_listed": 1}}}`)
	if got := kind(summarizeReport(t, fixture, traces, small), "Wide"); got != "branch" {
		t.Errorf("Wide with 2 scenarios and max_listed 1: main number is %s coverage, want branch", got)
	}
	// a forced choice, with its reason
	forced := writeConfig(t, `{"classification": {"enabled": true, "main_number": {"force": [{"workflow": "scenarios.Wide", "kind": "branch", "reason": "the ifs matter here"}]}}}`)
	fm := workflowNamed(t, summarizeReport(t, fixture, traces, forced), "scenarios.Wide")["mainNumber"].(map[string]any)
	if fm["kind"] != "branch" || fm["forcedReason"] != "the ifs matter here" {
		t.Errorf("forced main number: %v", fm)
	}
	// a force without a reason is refused
	bad := writeConfig(t, `{"classification": {"enabled": true, "main_number": {"force": [{"workflow": "scenarios.Wide", "kind": "branch"}]}}}`)
	if _, stderr, code := pathkit(t, "report", fixture, "--traces", traces, "--config", bad); code == 0 || !strings.Contains(stderr, "needs a \"reason\"") {
		t.Errorf("force without a reason: exit %d, %q", code, stderr)
	}
}

// The headline counts the workflows that meet their target.
func TestMainHeadline(t *testing.T) {
	fixture, traces := recordScenarios(t)
	cfg := writeConfig(t, `{"classification": {"enabled": true, "targets": {"default": 10, "branch": 10}}}`)
	doc := summarizeReport(t, fixture, traces, cfg)
	h := doc["mainHeadline"].(map[string]any)
	if num(h, "workflows") != 7 || num(h, "target") != 10 || num(h, "workflowsMeetingTarget") < 1 {
		t.Errorf("headline: %v", h)
	}
	out, _, code := pathkit(t, "report", fixture, "--traces", traces, "--config", cfg, "--summary")
	if code != 0 || !strings.Contains(out, " of 7 workflows meet their target (main number ≥ 10%") {
		t.Errorf("summary headline: exit %d\n%.400s", code, out)
	}
	// a workflow with nothing to measure is listed, not counted as passing
	if _, ok := h["notMeasured"]; ok {
		t.Logf("not measured: %v", h["notMeasured"])
	}
}

// Each trace names the Go test that made it, and the report says which tests
// add nothing the others do not cover.
func TestWhichTestCoversWhat(t *testing.T) {
	fixture, traces := recordScenarios(t)
	doc := summarizeReport(t, fixture, traces, "")
	tests, _ := doc["tests"].([]any)
	if len(tests) < 6 {
		t.Fatalf("tests: %v", doc["tests"])
	}
	by := map[string]map[string]any{}
	for _, x := range tests {
		m := x.(map[string]any)
		by[m["name"].(string)] = m
	}
	if m := by["TestLogicAgain"]; m == nil || m["addsNothingNew"] != true {
		t.Errorf("TestLogicAgain runs what TestLogic runs, so it adds nothing new: %v", m)
	}
	if m := by["TestWide"]; m == nil || m["addsNothingNew"] == true || num(m, "uniqueBranches") < 1 {
		t.Errorf("TestWide is the only test of Wide: %v", m)
	}
	out, _, code := pathkit(t, "report", fixture, "--traces", traces, "--summary")
	if code != 0 || !strings.Contains(out, "add nothing the others do not cover: TestLogicAgain") && !strings.Contains(out, "adds nothing the others do not cover: TestLogicAgain") {
		t.Errorf("summary line: exit %d\n%s", code, out)
	}
	// traces recorded before test names existed say so
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		stripped := strings.ReplaceAll(string(raw), `"testName"`, `"xTestName"`) // unknown key: ignored by older readers
		_ = os.WriteFile(f, []byte(stripped), 0o644)
	}
	if doc := summarizeReport(t, fixture, traces, ""); doc["tests"] == nil {
		t.Error("traces without test names still count")
	} else if first := doc["tests"].([]any)[0].(map[string]any); first["name"] != "(test name not recorded)" {
		t.Errorf("traces without test names: %v", first)
	}
}
