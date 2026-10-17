package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const defectFixture = "../../testdata/shapes/knowndefect"

func defectConfig(t *testing.T, rules string) string {
	t.Helper()
	return writeConfig(t, `{"packages": [`+jsonString(absPath(t, defectFixture))+`], "classification": {"enabled": true, "rules": [`+rules+`]}}`)
}

const defectRule = `{"id": "eu-blocked", "workflow": "knowndefect.BugBlockedBranch", "label": "known-defect",
  "when": ["if region == \"eu\" --true"], "reason": "region is overwritten by a bug"}`

// known-defect: needs-review, and the summary says how many paths a bug blocks.
func TestKnownDefectLineAndJSON(t *testing.T) {
	cfg := defectConfig(t, defectRule)
	traces := writeTracesFor(t, defectFixture, "knowndefect.BugBlockedBranch", []string{"J1.true", "J2.false"})
	out, stderr, code := run(t, "report", absPath(t, defectFixture), "--traces", traces, "--config", cfg, "--no-color")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "  Blocked by known bugs: 2\n") || !strings.Contains(out, "known-defect: 2 [auto 0 | manual 2]") {
		t.Errorf("no blocked-by-known-bugs line:\n%s", out)
	}
	if !strings.Contains(out, "Needs review:        2") {
		t.Errorf("the two paths are not needs-review:\n%s", out)
	}
	js, _, _ := run(t, "report", absPath(t, defectFixture), "--traces", traces, "--config", cfg, "--json")
	if m := decode(t, js); m["blockedByKnownBugs"] != float64(2) {
		t.Errorf("blockedByKnownBugs = %v, want 2", m["blockedByKnownBugs"])
	}
	// without the rule: no line, no key
	none := writeConfig(t, `{"packages": [`+jsonString(absPath(t, defectFixture))+`], "classification": {"enabled": true}}`)
	out, _, _ = run(t, "report", absPath(t, defectFixture), "--traces", traces, "--config", none, "--no-color")
	if strings.Contains(out, "Blocked by known bugs") {
		t.Errorf("the line is there with no known-defect entry:\n%s", out)
	}
}

// A rule with known-defect needs a reason like any rule, and "TODO" is refused.
func TestKnownDefectNeedsAReason(t *testing.T) {
	cfg := defectConfig(t, strings.Replace(defectRule, `"region is overwritten by a bug"`, `"TODO"`, 1))
	_, stderr, code := run(t, "rules", "check", absPath(t, defectFixture), "--config", cfg)
	if code != ExitError || !strings.Contains(stderr, `still has the reason "TODO"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestRulesCheck(t *testing.T) {
	fixture := absPath(t, defectFixture)
	traces := writeTracesFor(t, defectFixture, "knowndefect.BugBlockedBranch", []string{"J1.true", "J2.false"})

	t.Run("clean", func(t *testing.T) {
		out, stderr, code := run(t, "rules", "check", fixture, "--traces", traces, "--config", defectConfig(t, defectRule))
		if code != ExitOK || !strings.Contains(out, `rule "eu-blocked" [known-defect] in knowndefect.BugBlockedBranch: hits 2 paths`) || strings.Contains(out, "warning") {
			t.Errorf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("refused when a test ran the path", func(t *testing.T) {
		ran := writeTracesFor(t, defectFixture, "knowndefect.BugBlockedBranch", []string{"J1.true", "J2.true"})
		out, stderr, code := run(t, "rules", "check", fixture, "--traces", ran, "--config", defectConfig(t, defectRule))
		if code != ExitError || !strings.Contains(out, "REFUSED: rule \"eu-blocked\"") || !strings.Contains(out, "1 recorded run") || !strings.Contains(stderr, "1 entry refused") {
			t.Errorf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("a rule that hits nothing is a warning", func(t *testing.T) {
		rule := strings.Replace(defectRule, `if region == \"eu\" --true`, `if region == \"asia\" --true`, 1)
		out, _, code := run(t, "rules", "check", fixture, "--traces", traces, "--config", defectConfig(t, rule))
		if code != ExitOK || !strings.Contains(out, "warning: rule \"eu-blocked\" hits no path") || !strings.Contains(out, "hits 0 paths") {
			t.Errorf("code=%d out=%q", code, out)
		}
	})
	t.Run("two rules on the same path are a warning, once", func(t *testing.T) {
		second := strings.Replace(defectRule, "eu-blocked", "eu-again", 1)
		out, _, code := run(t, "rules", "check", fixture, "--traces", traces, "--config", defectConfig(t, defectRule+","+second))
		if code != ExitOK || strings.Count(out, "hit the same path") != 1 {
			t.Errorf("code=%d out=%q", code, out)
		}
	})
	t.Run("json", func(t *testing.T) {
		out, _, _ := run(t, "rules", "check", fixture, "--traces", traces, "--json", "--config", defectConfig(t, defectRule))
		m := decode(t, out)
		es := m["entries"].([]any)
		if len(es) != 1 || es[0].(map[string]any)["hits"] != float64(2) || len(m["refused"].([]any)) != 0 {
			t.Errorf("%s", out)
		}
	})
	t.Run("classification off", func(t *testing.T) {
		cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`]}`)
		_, stderr, code := run(t, "rules", "check", fixture, "--traces", traces, "--config", cfg)
		if code != ExitError || !strings.Contains(stderr, "classification is not enabled") {
			t.Errorf("code=%d stderr=%q", code, stderr)
		}
	})
}

// suggest drafts a rule for each group of needs-review paths. The draft has
// the reason TODO, and a config that holds it as it is gets refused.
func TestSuggestDraftsAreRefusedUntilEdited(t *testing.T) {
	fixture := absPath(t, "../../testdata/shapes/typeswitch")
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true}}`)
	out, stderr, code := run(t, "suggest", fixture, "--config", cfg, "--traces", t.TempDir())
	if code != ExitOK || !strings.Contains(out, `"reason": "TODO"`) || !strings.Contains(out, `"label": "TODO"`) || !strings.Contains(out, "unknown-input-type") {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
	}
	js, _, _ := run(t, "suggest", fixture, "--config", cfg, "--traces", t.TempDir(), "--json")
	sugs := decode(t, js)["suggestions"].([]any)
	if len(sugs) == 0 {
		t.Fatalf("no suggestions: %s", js)
	}
	rule := sugs[0].(map[string]any)["rule"].(map[string]any)
	// paste the draft into a config as it is: refused
	b := `{"packages": [` + jsonString(fixture) + `], "classification": {"enabled": true, "rules": [` + marshalJSON(t, rule) + `]}}`
	bad := writeConfig(t, b)
	_, stderr, code = run(t, "rules", "check", fixture, "--config", bad)
	if code != ExitError || !strings.Contains(stderr, `still has the label "TODO"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	rule["label"] = "known-defect"
	half := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true, "rules": [`+marshalJSON(t, rule)+`]}}`)
	if _, stderr, code = run(t, "rules", "check", fixture, "--config", half); code != ExitError || !strings.Contains(stderr, `still has the reason "TODO"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	// with a real reason it loads and hits the path it was drafted for
	rule["reason"] = "the input never holds this type"
	good := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true, "rules": [`+marshalJSON(t, rule)+`]}}`)
	out, stderr, code = run(t, "rules", "check", fixture, "--config", good, "--traces", t.TempDir())
	if code != ExitOK || !strings.Contains(out, "hits 1 path") {
		t.Errorf("code=%d out=%q stderr=%q", code, out, stderr)
	}
	// nothing needs review: nothing to draft
	clean := absPath(t, defectFixture)
	cfg2 := writeConfig(t, `{"packages": [`+jsonString(clean)+`], "classification": {"enabled": true}}`)
	out, _, _ = run(t, "suggest", clean, "--config", cfg2, "--traces", t.TempDir())
	if !strings.Contains(out, "nothing to draft") {
		t.Errorf("out=%q", out)
	}
}

func TestReportGroups(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, _, code := run(t, "report", "./orders", "--traces", traces, "--groups", "--no-color")
	if code != ExitOK || !strings.Contains(out, "Missed paths by first untested step: 1 testable path in 1 group (the biggest 1 are listed)") ||
		!strings.Contains(out, "first untested step: ChargeCard (activity) --success  (orders.go:") || !strings.Contains(out, "the tests get this far: if in.AmountCents <= 0 --false") ||
		!strings.Contains(out, "how to cover it: let activity ChargeCard succeed") {
		t.Errorf("code=%d\n%s", code, out)
	}
	plain, _, _ := run(t, "report", "./orders", "--traces", traces, "--no-color")
	if strings.Contains(plain, "Missed paths by first untested step") {
		t.Error("the section is there without --groups")
	}
	js, _, _ := run(t, "report", "./orders", "--traces", traces, "--groups", "--json")
	gs := decode(t, js)["missedGroups"].([]any)
	g := gs[0].(map[string]any)
	if len(gs) != 1 || g["paths"] != float64(1) || g["file"] != "orders.go" || g["firstUntestedStep"] != "ChargeCard (activity) --success" {
		t.Errorf("%s", js)
	}
	if strings.Contains(plainJSON(t), "missedGroups") {
		t.Error("the JSON has missedGroups without --groups")
	}
}

func plainJSON(t *testing.T) string {
	t.Helper()
	out, _, _ := run(t, "report", "./orders", "--traces", ordersTraces(t), "--json")
	return out
}

func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonMarshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// Every report format shows the assumptions panel when classification is on:
// all three assumptions, on or off, with a claim count. Off: nothing new.
func TestAssumptionPanelInEveryFormat(t *testing.T) {
	cfg := defectConfig(t, defectRule)
	fixture := absPath(t, defectFixture)
	traces := writeTracesFor(t, defectFixture, "knowndefect.BugBlockedBranch", []string{"J1.true", "J2.false"})
	names := []string{"closed-signal-channels", "default-json-converter", "no-custom-interceptor"}

	for _, cmd := range []string{"report", "coverage"} {
		target := fixture
		if cmd == "coverage" {
			target = filepath.Join(fixture, "knowndefect.go")
		}
		out, _, _ := run(t, cmd, target, "--traces", traces, "--config", cfg)
		if !strings.Contains(out, "Assumptions the claims rest on (") {
			t.Errorf("%s text has no panel:\n%s", cmd, out)
		}
		for _, n := range names {
			if !strings.Contains(out, "  "+n+":") {
				t.Errorf("%s text: %s missing", cmd, n)
			}
		}
		js, _, _ := run(t, cmd, target, "--traces", traces, "--config", cfg, "--json")
		panel, _ := decode(t, js)["assumptionPanel"].([]any)
		if len(panel) != 3 {
			t.Errorf("%s json: assumptionPanel has %d entries, want 3", cmd, len(panel))
		}
	}
	// --summary still shows it
	out, _, _ := run(t, "report", fixture, "--traces", traces, "--config", cfg, "--summary", "--no-color")
	if !strings.Contains(out, "Assumptions the claims rest on (") {
		t.Errorf("--summary has no panel:\n%s", out)
	}
	// HTML
	page := filepath.Join(t.TempDir(), "r.html")
	if _, stderr, code := run(t, "report", fixture, "--traces", traces, "--config", cfg, "--html="+page); code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if !strings.Contains(string(b), `data-assumption="`+n+`" data-on=`) {
			t.Errorf("html: panel row for %s missing", n)
		}
	}
	// classification off: no panel anywhere
	off := writeConfig(t, `{"packages": [`+jsonString(fixture)+`]}`)
	out, _, _ = run(t, "report", fixture, "--traces", traces, "--config", off, "--no-color")
	js, _, _ := run(t, "report", fixture, "--traces", traces, "--config", off, "--json")
	if strings.Contains(out, "Assumptions the claims rest on") || strings.Contains(js, "assumptionPanel") {
		t.Error("the panel is there with classification off")
	}
}

// A rule that hits nothing is warned about once by rules check, not twice
// (the loader's stale-entry finding is left out of rules check).
func TestRulesCheckWarnsOnceAboutAStaleRule(t *testing.T) {
	rule := strings.Replace(defectRule, `if region == \"eu\" --true`, `if region == \"asia\" --true`, 1)
	out, stderr, code := run(t, "rules", "check", absPath(t, defectFixture), "--traces", t.TempDir(), "--config", defectConfig(t, rule))
	if code != ExitOK || strings.Count(out+stderr, "eu-blocked") != 2 /* the list line and the warning */ || strings.Contains(stderr, "stale rule") {
		t.Errorf("code=%d out=%q stderr=%q", code, out, stderr)
	}
	// report still prints the finding, as before
	_, stderr, _ = run(t, "report", absPath(t, defectFixture), "--traces", writeTracesFor(t, defectFixture, "knowndefect.BugBlockedBranch", []string{"J1.true", "J2.false"}), "--config", defectConfig(t, rule))
	if !strings.Contains(stderr, "stale rule") {
		t.Errorf("report lost the stale-rule warning: %q", stderr)
	}
}

// With a workflow over the cap, the headline shows three numbers and leaves
// the listed paths of that workflow out of the path number. Without one, the
// headline is the plain totals and the JSON has no headline key.
func TestHeadlineThreeNumbers(t *testing.T) {
	big := absPath(t, "../../testdata/fixtures/bigflow")
	cfg := writeConfig(t, `{"packages": [`+jsonString(big)+`], "classification": {"enabled": true}}`)
	traces := writeTracesFor(t, "../../testdata/fixtures/bigflow", "bigflow.TwentyFourIfs", []string{"J9.nowhere"})
	for _, args := range [][]string{{"report", big}, {"report", big, "--summary"}} {
		out, stderr, code := run(t, append(args, "--traces", traces, "--config", cfg, "--no-color")...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d stderr=%q", args, code, stderr)
		}
		for _, want := range []string{"Path coverage (testable): ", " too big for path coverage, see stage coverage", "Stage coverage (testable): ", "Branch coverage: "} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: headline lacks %q", args, want)
			}
		}
		if strings.Contains(out, "paths total ·") {
			t.Errorf("%v: the listed-path total is still in the headline", args)
		}
	}
	js, _, _ := run(t, "report", big, "--traces", traces, "--config", cfg, "--json")
	m := decode(t, js)
	h, ok := m["headline"].(map[string]any)
	if !ok || h["workflowsTooBigForPathCoverage"].(float64) < 1 || h["stageCoverage"].(map[string]any)["total"].(float64) == 0 {
		t.Errorf("headline in JSON: %v", m["headline"])
	}
	if _, ok := m["total"]; !ok { // the old keys are still there, listed paths only
		t.Error("the old total key is gone")
	}
	page := filepath.Join(t.TempDir(), "r.html")
	if _, stderr, code := run(t, "report", big, "--traces", traces, "--config", cfg, "--html="+page); code != ExitOK {
		t.Fatalf("html: code=%d stderr=%q", code, stderr)
	}
	b, _ := os.ReadFile(page)
	for _, want := range []string{`data-card="path-coverage"`, `data-card="stage-coverage"`, `data-card="branch-coverage"`, `class="total headline"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("html lacks %s", want)
		}
	}
	if strings.Contains(string(b), `data-card="paths"`) {
		t.Error("html still has the listed-paths card")
	}
}

func TestHeadlineUnchangedUnderTheCap(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, _, _ := run(t, "report", "./orders", "--traces", traces, "--no-color")
	if !strings.Contains(out, "paths total · ") || strings.Contains(out, "Path coverage") {
		t.Errorf("a project under the cap changed its headline:\n%s", out)
	}
	js, _, _ := run(t, "report", "./orders", "--traces", traces, "--json")
	if _, has := decode(t, js)["headline"]; has {
		t.Error("headline key without a workflow over the cap")
	}
}
