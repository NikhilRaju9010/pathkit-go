package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ciConfig writes a config for the pilot's orders workflow (2 of its 3
// paths have a test, path 3 has none) with the given classification part.
func ciConfig(t *testing.T, classification string) string {
	t.Helper()
	return writeConfig(t, `{"packages": [`+jsonString(absPath(t, pilot+"/orders"))+`], "classification": `+classification+`}`)
}

// ordersStableIDs are orders.OrderWorkflow's stable ids by path number.
func ordersStableIDs(t *testing.T, traces, cfg string) map[int]string {
	t.Helper()
	out, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	ids := map[int]string{}
	for _, p := range decode(t, out)["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		pm := p.(map[string]any)
		ids[int(pm["number"].(float64))] = pm["stableId"].(string)
	}
	return ids
}

func TestCIOffChangesNothing(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	// No classification: the CI flags are refused, never silently ignored.
	for _, args := range [][]string{{"--baseline", "b.json"}, {"--write-baseline", "--baseline", "b.json"}, {"--allow-lost-tests"}} {
		_, stderr, code := run(t, append([]string{"report", "./orders", "--traces", traces}, args...)...)
		if code != ExitError || !strings.Contains(stderr, "--baseline, --write-baseline and --allow-lost-tests need path classification") {
			t.Errorf("%v: code=%d stderr=%q", args, code, stderr)
		}
	}
	// A ci part with classification off: a warning, no check, exit 0.
	cfg := ciConfig(t, `{"ci": {"baseline": "missing.json", "fail_under_testable": 100}}`)
	out, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--json")
	if code != ExitOK || !strings.Contains(stderr, "so classification.ci in") || strings.Contains(out, `"ci"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	// --fail-under alone prints exactly what it printed before.
	_, stderr, code = run(t, "report", "./orders", "--traces", traces, "--fail-under", "80")
	if code != ExitBelowThreshold || stderr != "pathkit report: coverage 66.7% is below --fail-under 80%\n" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestCIBaseline(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	cfg := ciConfig(t, `{"enabled": true, "ci": {"baseline": "ci/pathkit-baseline.json"}}`)
	file := filepath.Join(filepath.Dir(cfg), "ci", "pathkit-baseline.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	ids := ordersStableIDs(t, traces, ciConfig(t, `{"enabled": true}`))

	// No baseline file yet: a real error, before anything is decided.
	_, stderr, code := run(t, "report", "--traces", traces, "--config", cfg)
	if code != ExitError || !strings.Contains(stderr, "not found; write it with --write-baseline") {
		t.Fatalf("missing baseline: code=%d stderr=%q", code, stderr)
	}

	// Write it: relative to the config file's folder.
	_, stderr, code = run(t, "report", "--traces", traces, "--config", cfg, "--write-baseline")
	if code != ExitOK || stderr != "pathkit report: wrote the baseline ci/pathkit-baseline.json: 3 paths in 1 workflow\n" {
		t.Fatalf("write: code=%d stderr=%q", code, stderr)
	}
	var written struct {
		Workflows []struct {
			Name  string
			Paths []struct {
				StableID string
				Bucket   string
				Covered  bool
			}
		}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if len(written.Workflows) != 1 || written.Workflows[0].Name != "orders.OrderWorkflow" || len(written.Workflows[0].Paths) != 3 {
		t.Fatalf("baseline file:\n%s", raw)
	}

	// The same run against it: path 3 is a known gap, so it passes.
	out, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--json")
	if code != ExitOK || stderr != "" {
		t.Fatalf("check: code=%d stderr=%q", code, stderr)
	}
	ci := decode(t, out)["ci"].(map[string]any)
	if ci["passed"] != true || ci["baselineMode"] != "check" || len(ci["newUntested"].([]any)) != 0 {
		t.Errorf("ci = %v", ci)
	}

	// Path 3 removed from the baseline: it is new and has no test.
	text := string(raw)
	start := strings.Index(text, `{`+"\n"+`          "stableId": "`+ids[3]+`"`)
	if start < 0 {
		t.Fatalf("path 3 (%s) not found in\n%s", ids[3], text)
	}
	end := start + strings.Index(text[start:], "}") + 1
	trimmed := text[:start] + text[end:]
	trimmed = strings.Replace(trimmed, ",\n        \n", "\n", 1)
	trimmed = strings.Replace(trimmed, "},\n        ,", "},", 1)
	trimmed = strings.Replace(trimmed, ",\n        ]", "\n      ]", 1)
	trimmed = strings.Replace(trimmed, "[\n        ,", "[", 1)
	if err := os.WriteFile(file, []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, "report", "--traces", traces, "--config", cfg)
	want := "pathkit report: 1 testable path with no test not in the baseline ci/pathkit-baseline.json:\n" +
		"  orders.OrderWorkflow path 3 (" + ids[3] + ", new): Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)\n"
	if code != ExitBelowThreshold || stderr != want {
		t.Fatalf("new untested: code=%d stderr=%q\nwant %q\nfile:\n%s", code, stderr, want, trimmed)
	}
	if !strings.Contains(stdout, "2/3 paths") {
		t.Errorf("the report must still be printed first:\n%s", stdout)
	}

	// --write-baseline narrowed by flags would drop workflows: refused.
	for _, args := range [][]string{{"--exclude", "OrderWorkflow"}, {"--include", "OrderWorkflow"}} {
		_, stderr, code = run(t, append([]string{"report", "--traces", traces, "--config", cfg, "--write-baseline"}, args...)...)
		if code != ExitError || !strings.Contains(stderr, "--write-baseline cannot be used with "+args[0]) {
			t.Errorf("%v: code=%d stderr=%q", args, code, stderr)
		}
	}
	// Only report with no folder measures the whole scope (a sub-folder
	// a sub-folder used to write a partial baseline over a good one).
	_, stderr, code = run(t, "report", "./orders", "--traces", traces, "--config", cfg, "--write-baseline")
	if code != ExitError || !strings.Contains(stderr, "--write-baseline cannot be used with a folder argument: the baseline must hold every workflow in the config's scope, so run \"pathkit report --write-baseline\" with no folder") {
		t.Errorf("folder: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = run(t, "coverage", "./orders/orders.go", "--traces", traces, "--config", cfg, "--write-baseline")
	if code != ExitError || !strings.Contains(stderr, "unknown flag: --write-baseline") {
		t.Errorf("coverage: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = run(t, "report", "--traces", traces, "--config", cfg, "--all", "--baseline", file)
	if code != ExitError || !strings.Contains(stderr, "cannot be used with --all: --all ignores the config") {
		t.Errorf("--all: code=%d stderr=%q", code, stderr)
	}
	// coverage checks against a baseline too.
	_, stderr, code = run(t, "coverage", "./orders/orders.go", "--traces", traces, "--config", cfg)
	if code != ExitBelowThreshold || !strings.Contains(stderr, "1 testable path with no test not in the baseline") {
		t.Errorf("coverage check: code=%d stderr=%q", code, stderr)
	}
	noFile := ciConfig(t, `{"enabled": true}`)
	_, stderr, code = run(t, "report", "--traces", traces, "--config", noFile, "--write-baseline")
	if code != ExitError || !strings.Contains(stderr, "--write-baseline needs a file") {
		t.Errorf("no file: code=%d stderr=%q", code, stderr)
	}
}

// A path that becomes needs-review fails; the floor checks testable
// coverage on its own, next to --fail-under, and both are printed.
func TestCINeedsReviewAndFloor(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "b.json")
	plain := ciConfig(t, `{"enabled": true}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", plain, "--write-baseline", "--baseline", base); code != ExitOK {
		t.Fatalf("write: %q", stderr)
	}
	ids := ordersStableIDs(t, traces, plain)

	// A rule's claim is not trusted while the workflow has a trace that fits
	// no path: path 3 becomes needs-review (model-limit), which was
	// testable in the baseline.
	raw, err := os.ReadFile(filepath.Join(traces, "a.trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tr map[string]any
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	tr["steps"] = []string{"J1.false", "J2.nowhere"}
	writeTrace(t, traces, "odd", tr)
	unsure := ciConfig(t, `{"enabled": true, "rules": [{"id": "never-charged", "workflow": "OrderWorkflow", "label": "contradictory",
	  "when": ["ChargeCard (activity) --success"], "impossible": true, "reason": "made up for the test"}]}`)
	out, stderr, code := run(t, "report", "--traces", traces, "--config", unsure, "--baseline", base, "--json")
	if code != ExitBelowThreshold || !strings.Contains(stderr, "pathkit report: 1 needs-review path not in the baseline "+base+":\n  orders.OrderWorkflow path 3 ("+ids[3]+", was testable): ") {
		t.Fatalf("needs-review: code=%d stderr=%q", code, stderr)
	}
	ci := decode(t, out)["ci"].(map[string]any)
	if ci["passed"] != false || len(ci["newNeedsReview"].([]any)) != 1 || len(ci["newUntested"].([]any)) != 0 {
		t.Errorf("ci = %v", ci)
	}
	if err := os.Remove(filepath.Join(traces, "odd.trace.json")); err != nil {
		t.Fatal(err)
	}

	floor := ciConfig(t, `{"enabled": true, "ci": {"fail_under_testable": 70}}`)
	_, stderr, code = run(t, "report", "--traces", traces, "--config", floor)
	if want := "pathkit report: testable coverage 66.7% is below classification.ci.fail_under_testable 70% (in " + floor + ")\n"; code != ExitBelowThreshold || stderr != want {
		t.Errorf("floor: code=%d stderr=%q\nwant %q", code, stderr, want)
	}
	_, stderr, code = run(t, "report", "--traces", traces, "--config", floor, "--fail-under", "80")
	if code != ExitBelowThreshold || !strings.Contains(stderr, "coverage 66.7% is below --fail-under 80% (basis: testable)\n") || !strings.Contains(stderr, "testable coverage 66.7% is below classification.ci.fail_under_testable 70%") {
		t.Errorf("both: code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code = run(t, "report", "--traces", traces, "--config", ciConfig(t, `{"enabled": true, "ci": {"fail_under_testable": 66.6}}`)); code != ExitOK {
		t.Errorf("above the floor: code=%d stderr=%q", code, stderr)
	}
}

// The baseline remembers where each numbered rule entry ("#2")
// pointed; a later check warns when its neighbourhood changed, because the
// number may now name another decision. A warning only: the exit code is
// not changed by it.
func TestCIRuleAnchors(t *testing.T) {
	folder := absPath(t, "../../testdata/fixtures/rulewithout")
	traces := writeTracesFor(t, "../../testdata/fixtures/rulewithout", "rulewithout.TwiceWorkflow", []string{"J1.false", "J3.false"})
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true,
	  "ci": {"baseline": "b.json"},
	  "rules": [{"id": "second-needs-first", "workflow": "TwiceWorkflow", "label": "contradictory",
	    "when": ["if a || b #2 --true"], "without": ["if a || b #1 --true"], "impossible": true, "reason": "made up for the test"}]}}`)
	file := filepath.Join(filepath.Dir(cfg), "b.json")
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--write-baseline"); code != ExitOK {
		t.Fatalf("write: code=%d stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"ruleAnchors": [`,
		`"entry": "if a || b #1 --true",
      "before": [
        "Start"
      ],
      "after": [
        "--true--> first (activity)",
        "--false--> if a || b #2"
      ]`,
		`"entry": "if a || b #2 --true",
      "before": [
        "first (activity) --success",
        "if a || b --false"
      ],
      "after": [
        "--true--> second (activity)",
        "--false--> End (completed)"
      ]`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("baseline file lacks %s:\n%s", want, raw)
		}
	}
	check := func() (string, int) {
		_, stderr, code := run(t, "report", "--traces", traces, "--config", cfg)
		return stderr, code
	}
	stderr, code := check()
	if strings.Contains(stderr, "may point at a different decision") {
		t.Errorf("unchanged code warned: %q", stderr)
	}
	// As if an identical decision had been added above: the stored
	// neighbourhood no longer matches.
	moved := strings.Replace(string(raw), `"first (activity) --success",
        "if a || b --false"`, `"Start"`, 1)
	if err := os.WriteFile(file, []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr2, code2 := check()
	wantWarn := `pathkit report: warning: rule "second-needs-first" (rulewithout.TwiceWorkflow) entry "if a || b #2 --true": since the baseline was written, the decision it names changed place (it came right after "Start", now after "first (activity) --success", "if a || b --false"). An identical decision may have been added, removed or moved, so the number may point at a different decision now; check the rule, then write the baseline again` + "\n"
	if !strings.Contains(stderr2, wantWarn) || code2 != code {
		t.Errorf("moved: code=%d (was %d) stderr=%q", code2, code, stderr2)
	}
	// A baseline written before anchors existed: nothing to compare.
	i := strings.Index(string(raw), `,
  "ruleAnchors"`)
	if i < 0 {
		t.Fatal("no ruleAnchors in the file")
	}
	old := string(raw)[:i] + "\n}\n"
	if old == string(raw) {
		t.Fatal("could not remove the anchors from the file")
	}
	if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if stderr, _ := check(); strings.Contains(stderr, "may point at a different decision") || strings.Contains(stderr, "baseline file") {
		t.Errorf("old baseline: %q", stderr)
	}
}

// A path that had a test in the baseline and lost it
// fails the CI check, naming the path; --allow-lost-tests lets it pass and
// keeps it as a warning.
func TestCILostTest(t *testing.T) {
	traces := ordersTraces(t) // paths 1 and 2 have a test
	inPilot(t)
	base := filepath.Join(t.TempDir(), "b.json")
	cfg := ciConfig(t, `{"enabled": true}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--write-baseline", "--baseline", base); code != ExitOK {
		t.Fatalf("write: %q", stderr)
	}
	ids := ordersStableIDs(t, traces, cfg)
	if err := os.Remove(filepath.Join(traces, "b.trace.json")); err != nil { // path 2's run
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--baseline", base)
	want := "pathkit report: 1 path had a test in the baseline " + base + " and has none now:\n" +
		"  orders.OrderWorkflow path 2 (" + ids[2] + ", now testable): Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)\n" +
		"pathkit report: orders.OrderWorkflow: main number (path coverage) fell from 66.7% (2/3) in the baseline " + base + " to 33.3% (1/3)\n"
	// The baseline was written by this PathKit, so it holds branch coverage and its
	// checks come first (M11, B6); the lines about lost tests and the main number
	// are exactly the ones they always were.
	if code != ExitBelowThreshold || !strings.HasSuffix(stderr, want) || !strings.Contains(stderr, "branch coverage 25.0% (1 of 4 decision outcomes) is below classification.ci.branch_floor 75%") || !strings.Contains(stdout, "1/3 paths") {
		t.Fatalf("lost: code=%d stderr=%q\nwant it to end with %q", code, stderr, want)
	}
	out, _, _ := run(t, "report", "--traces", traces, "--config", cfg, "--baseline", base, "--json")
	ci := decode(t, out)["ci"].(map[string]any)
	if ci["passed"] != false || len(ci["lostTests"].([]any)) != 1 || ci["lostTestsAllowed"] != false {
		t.Errorf("json: %v", ci)
	}

	_, stderr, code = run(t, "report", "--traces", traces, "--config", cfg, "--baseline", base, "--allow-lost-tests")
	wantWarn := "pathkit report: warning: 1 path had a test in the baseline " + base + " and has none now (allowed by --allow-lost-tests):\n" +
		"  orders.OrderWorkflow path 2 (" + ids[2] + ", now testable): "
	if code != ExitOK || !strings.HasPrefix(stderr, wantWarn) {
		t.Errorf("allowed: code=%d stderr=%q", code, stderr)
	}
	// The flag is refused where it can do nothing.
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--allow-lost-tests"); code != ExitError || !strings.Contains(stderr, "--allow-lost-tests needs a baseline to compare with") {
		t.Errorf("no baseline: code=%d stderr=%q", code, stderr)
	}
	out, _, _ = run(t, "coverage", "./orders/orders.go", "--traces", traces, "--config", cfg, "--baseline", base, "--allow-lost-tests", "--json")
	ci = decode(t, out)["ci"].(map[string]any)
	if ci["passed"] != true || len(ci["lostTests"].([]any)) != 1 || ci["lostTestsAllowed"] != true {
		t.Errorf("allowed json: %v", ci)
	}
}

// The main number is part of the baseline, and the check fails when a
// workflow's main number is below classification.ci.main_floor or has fallen
// below the baseline's by more than classification.ci.tolerance. An old
// baseline (no main numbers) is checked as before.
func TestCIMainNumber(t *testing.T) {
	traces := ordersTraces(t) // 2 of 3 paths: path coverage 66.7%
	inPilot(t)
	base := filepath.Join(t.TempDir(), "b.json")
	cfg := ciConfig(t, `{"enabled": true}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--write-baseline", "--baseline", base); code != ExitOK {
		t.Fatalf("write: %q", stderr)
	}
	raw, _ := os.ReadFile(base)
	if !strings.Contains(string(raw), `"mainNumber"`) || !strings.Contains(string(raw), `"kind": "path"`) {
		t.Fatalf("the baseline has no main number:\n%s", raw)
	}

	// a floor above the workflow's main number
	floorCfg := ciConfig(t, `{"enabled": true, "ci": {"main_floor": 80}}`)
	_, stderr, code := run(t, "report", "--traces", traces, "--config", floorCfg, "--baseline", base)
	if code != ExitBelowThreshold || !strings.Contains(stderr, "orders.OrderWorkflow: main number (path coverage 2/3) 66.7% is below classification.ci.main_floor 80%") {
		t.Errorf("floor: code=%d stderr=%q", code, stderr)
	}
	// a floor the workflow meets
	okCfg := ciConfig(t, `{"enabled": true, "ci": {"main_floor": 60}}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", okCfg, "--baseline", base); code != ExitOK {
		t.Errorf("floor met: code=%d stderr=%q", code, stderr)
	}

	// an old baseline, without main numbers, is read and checked as before
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, w := range doc["workflows"].([]any) {
		delete(w.(map[string]any), "mainNumber")
	}
	old, _ := json.Marshal(doc)
	oldFile := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(oldFile, old, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--baseline", oldFile); code != ExitOK {
		t.Errorf("old baseline: code=%d stderr=%q", code, stderr)
	}

	// a bad value is refused
	bad := ciConfig(t, `{"enabled": true, "ci": {"main_floor": 120}}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", bad); code != ExitError || !strings.Contains(stderr, "main_floor") {
		t.Errorf("bad floor: code=%d stderr=%q", code, stderr)
	}
}

// The branch gates: ci.branch_floor, and a baseline that holds branch
// coverage (which brings the commendable band as a floor and fails a drop).
// An older baseline, with no branch data, keeps working exactly as before.
func TestCIBranchCoverage(t *testing.T) {
	traces := ordersTraces(t) // 3 of 4 decision outcomes: 75%, commendable
	inPilot(t)
	base := filepath.Join(t.TempDir(), "b.json")
	cfg := ciConfig(t, `{"enabled": true}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--write-baseline", "--baseline", base); code != ExitOK {
		t.Fatalf("write: %q", stderr)
	}
	raw, _ := os.ReadFile(base)
	if !strings.Contains(string(raw), `"branchCoverage"`) || !strings.Contains(string(raw), `"possible": 4`) {
		t.Fatalf("the baseline has no branch coverage:\n%s", raw)
	}
	// an explicit floor above the project's and the workflow's coverage
	high := ciConfig(t, `{"enabled": true, "ci": {"branch_floor": 80}}`)
	_, stderr, code := run(t, "report", "--traces", traces, "--config", high)
	if code != ExitBelowThreshold || !strings.Contains(stderr, "branch coverage 75.0% (3 of 4 decision outcomes) is below classification.ci.branch_floor 80%") ||
		!strings.Contains(stderr, "orders.OrderWorkflow: branch coverage 75.0%") {
		t.Errorf("floor: code=%d stderr=%q", code, stderr)
	}
	// a floor that is met
	low := ciConfig(t, `{"enabled": true, "ci": {"branch_floor": 70}}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", low); code != ExitOK {
		t.Errorf("floor met: code=%d stderr=%q", code, stderr)
	}
	// the baseline brings the commendable band (75) as the floor: met here
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", cfg, "--baseline", base); code != ExitOK {
		t.Errorf("baseline floor: code=%d stderr=%q", code, stderr)
	}
	// a team that asks for more in branches.grades raises that floor
	strict := writeConfig(t, `{"packages": [`+jsonString(absPath(t, pilot+"/orders"))+`], "classification": {"enabled": true}, "branches": {"grades": {"acceptable": 60, "commendable": 80, "exemplary": 95}}}`)
	_, stderr, code = run(t, "report", "--traces", traces, "--config", strict, "--baseline", base)
	if code != ExitBelowThreshold || !strings.Contains(stderr, "is below classification.ci.branch_floor 80% (in the commendable grade") {
		t.Errorf("strict grades: code=%d stderr=%q", code, stderr)
	}
	// a drop below the baseline: the baseline says 100%
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["branchCoverage"] = map[string]any{"covered": 4, "possible": 4, "percent": 100.0}
	for _, w := range doc["workflows"].([]any) {
		w.(map[string]any)["branchCoverage"] = map[string]any{"covered": 4, "possible": 4, "percent": 100.0}
	}
	dropped, _ := json.Marshal(doc)
	dropFile := filepath.Join(t.TempDir(), "drop.json")
	if err := os.WriteFile(dropFile, dropped, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = run(t, "report", "--traces", traces, "--config", cfg, "--baseline", dropFile)
	if code != ExitBelowThreshold || !strings.Contains(stderr, "branch coverage of the project fell from 100.0% (4/4)") {
		t.Errorf("drop: code=%d stderr=%q", code, stderr)
	}
	// ... unless the tolerance allows it
	tol := ciConfig(t, `{"enabled": true, "ci": {"tolerance": 30}}`)
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", tol, "--baseline", dropFile); code != ExitOK {
		t.Errorf("tolerance: code=%d stderr=%q", code, stderr)
	}
	// an older baseline without branch data: no branch check at all
	for _, w := range doc["workflows"].([]any) {
		delete(w.(map[string]any), "branchCoverage")
	}
	delete(doc, "branchCoverage")
	oldRaw, _ := json.Marshal(doc)
	oldFile := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(oldFile, oldRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", strict, "--baseline", oldFile); code != ExitOK {
		t.Errorf("old baseline: code=%d stderr=%q", code, stderr)
	}
	// json
	out, _, _ := run(t, "report", "--traces", traces, "--config", high, "--json")
	if !strings.Contains(out, `"branchFloor"`) {
		t.Errorf("json has no branchFloor:\n%s", out)
	}
	if _, stderr, code := run(t, "report", "--traces", traces, "--config", ciConfig(t, `{"enabled": true, "ci": {"branch_floor": 120}}`)); code != ExitError || !strings.Contains(stderr, "branch_floor") {
		t.Errorf("bad floor: code=%d %q", code, stderr)
	}
}
