package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// additiveKeys are the JSON fields added with path classification. Every
// other key and value must be exactly what it was before.
var additivePathKeys = []string{"stableId", "bucket", "label", "source", "confidence", "possibleInProduction", "explanation", "ruleId", "proof", "scenarioId", "sameBehaviourAs"}

func stripAdditive(v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range additivePathKeys {
			delete(x, k)
		}
		if _, ok := x["pathList"]; ok {
			for _, k := range []string{"scenarioId", "sameBehaviourAs", "mainNumber", "scenarios", "handlerCoverage", "decisionPairs"} { // scenarios and main numbers (M9)
				delete(x, k)
			}
			delete(x, "classification") // the per-workflow count
		}
		if _, ok := x["workflows"]; ok {
			delete(x, "classification") // the top-level count
			delete(x, "coverageBasis")
			delete(x, "classificationShown")
			delete(x, "tests")                                                     // what each Go test covered (M10.7)
			delete(x, "testsToWriteFirst")                                         // the advice (M10.1)
			delete(x, "mainHeadline")                                              // workflows meeting their target (M9.7)
			delete(x, "unread")                                                    // the "could not fully read" panel (M7.8)
			for _, k := range []string{"callOutcomes", "handlers", "loopRounds"} { // the outcome, handler and loop tables (M8)
				delete(x, k)
			}
		}
		if _, ok := x["threshold"]; ok {
			delete(x, "basis")
		}
		for _, c := range x {
			stripAdditive(c)
		}
	case []any:
		for _, c := range x {
			stripAdditive(c)
		}
	}
}

// The JSON only gained fields. Remove the new ones and what is left must
// equal the golden files written before classification existed
// (testdata/coverage_orders.json, report_orders.json, never edited).
func TestJSONOnlyGainedFields(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	old := func(name string) map[string]any {
		data, err := os.ReadFile(absPath(t, "../../internal/cli/testdata/"+name))
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, string(data))
	}
	cov, _, code := run(t, "coverage", "./orders/orders.go", "--traces", traces, "--json", "--fail-under", "60")
	if code != ExitOK {
		t.Fatalf("coverage exit %d", code)
	}
	got := decode(t, cov)
	stripAdditive(got)
	if want := old("coverage_orders.json"); !jsonEqual(got, want) {
		t.Errorf("coverage --json minus the new fields differs from the old golden\n got %v\nwant %v", got, want)
	}
	rep, _, code := run(t, "report", "./orders", "--traces", traces, "--json", "--fail-under", "60")
	if code != ExitOK {
		t.Fatalf("report exit %d", code)
	}
	got = decode(t, rep)
	stripAdditive(got)
	if want := old("report_orders.json"); !jsonEqual(got, want) {
		t.Errorf("report --json minus the new fields differs from the old golden\n got %v\nwant %v", got, want)
	}
}

// The new fields themselves, on the orders workflow (2 of 3 paths run).
func TestJSONClassificationFields(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for _, cmd := range []string{"coverage", "report"} {
		out, _, _ := run(t, cmd, "./orders", "--traces", traces, "--json", "--fail-under", "60")
		m := decode(t, out)
		if m["coverageBasis"] != "overall" || m["classificationShown"] != false {
			t.Errorf("%s: coverageBasis=%v classificationShown=%v, want overall and false", cmd, m["coverageBasis"], m["classificationShown"])
		}
		if fu := m["failUnder"].(map[string]any); fu["basis"] != "overall" {
			t.Errorf("%s: failUnder.basis = %v", cmd, fu["basis"])
		}
		c := m["classification"].(map[string]any)
		b := c["buckets"].(map[string]any)
		if b["testable"].(map[string]any)["paths"] != 3.0 || b["cannotBeTested"].(map[string]any)["paths"] != 0.0 {
			t.Errorf("%s: buckets %v", cmd, b)
		}
		for _, k := range []string{"testableCoverage", "specialTestCoverage", "overallCoverage"} {
			if _, ok := c[k]; !ok {
				t.Errorf("%s: classification.%s is missing", cmd, k)
			}
		}
		seen := map[string]bool{}
		for _, p := range m["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
			p := p.(map[string]any)
			id, _ := p["stableId"].(string)
			if !strings.HasPrefix(id, "s1:") || seen[id] || p["bucket"] != "testable" || p["source"] != "auto" || p["confidence"] != "certain" ||
				p["possibleInProduction"] != true || p["label"] != nil || p["ruleId"] != nil || p["explanation"] == "" {
				t.Errorf("%s: path %v", cmd, p)
			}
			seen[id] = true
		}
	}
}

// Until classification is switched on, the text has no new lines and
// --fail-under's message has no basis. (The exact old text is pinned by
// TestReportText, TestCoverageText and TestReportFailUnder, which pass unchanged.)
func TestNoClassificationOutputByDefault(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for _, cmd := range []string{"coverage", "report"} {
		out, stderr, code := run(t, cmd, "./orders", "--traces", traces, "--fail-under", "70")
		if code != ExitBelowThreshold || stderr != "pathkit "+cmd+": coverage 66.7% is below --fail-under 70%\n" {
			t.Errorf("%s: code=%d stderr=%q", cmd, code, stderr)
		}
		if strings.Contains(out, "Classification") || strings.Contains(out, "basis") {
			t.Errorf("%s printed classification output by default:\n%s", cmd, out)
		}
	}
}

func TestCoverageBasisFlag(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for _, cmd := range []string{"coverage", "report"} {
		for _, basis := range []string{"testable", "overall"} {
			out, stderr, code := run(t, cmd, "./orders", "--traces", traces, "--coverage-basis", basis)
			if code != ExitOK || stderr != "" {
				t.Fatalf("%s %s: code=%d stderr=%q", cmd, basis, code, stderr)
			}
			for _, want := range []string{"Classification:", "  Testable:            3", "Testable coverage:     2 / 3 = 66.7%  (testable paths only)",
				"Overall coverage:      2 / 3 = 66.7%  (all paths)", "Coverage basis used:   " + basis + "\n"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s --coverage-basis %s lacks %q:\n%s", cmd, basis, want, out)
				}
			}
		}
		_, stderr, code := run(t, cmd, "./orders", "--traces", traces, "--coverage-basis", "everything")
		if want := "pathkit " + cmd + ": invalid --coverage-basis value: \"everything\" (use testable or overall)\n"; code != ExitError || stderr != want {
			t.Errorf("%s bad value: code=%d stderr=%q, want %q", cmd, code, stderr, want)
		}
	}
}

// --fail-under names its basis whenever classification is in use, and the
// config's switch makes testable the default basis.
func TestFailUnderBasis(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	enabled := writeConfig(t, `{"classification": {"enabled": true}}`)
	disabled := writeConfig(t, `{"classification": {"enabled": false}}`)
	tests := []struct {
		name   string
		args   []string
		stderr string
		block  string // the basis line the report must print ("" for none)
	}{
		{"flag testable", []string{"--coverage-basis", "testable"}, "coverage 66.7% is below --fail-under 70% (basis: testable)\n", "testable"},
		{"flag overall", []string{"--coverage-basis", "overall"}, "coverage 66.7% is below --fail-under 70% (basis: overall)\n", "overall"},
		{"config enabled: testable by default", []string{"--config", enabled}, "coverage 66.7% is below --fail-under 70% (basis: testable)\n", "testable"},
		{"config enabled, flag overall", []string{"--config", enabled, "--coverage-basis", "overall"}, "coverage 66.7% is below --fail-under 70% (basis: overall)\n", "overall"},
		{"config disabled: exactly as before", []string{"--config", disabled}, "coverage 66.7% is below --fail-under 70%\n", ""},
	}
	for _, cmd := range []string{"coverage", "report"} {
		for _, tt := range tests {
			out, stderr, code := run(t, append([]string{cmd, "./orders", "--traces", traces, "--fail-under", "70"}, tt.args...)...)
			if code != ExitBelowThreshold || stderr != "pathkit "+cmd+": "+tt.stderr {
				t.Errorf("%s/%s: code=%d stderr=%q", cmd, tt.name, code, stderr)
			}
			has := strings.Contains(out, "Coverage basis used:   "+tt.block+" (for --fail-under)\n")
			if (tt.block != "") != has {
				t.Errorf("%s/%s: basis line present=%v, want %v:\n%s", cmd, tt.name, has, tt.block != "", out)
			}
		}
	}
}

func TestHTMLClassificationBlock(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	page := func(args ...string) string {
		file := t.TempDir() + "/r.html"
		if _, stderr, code := run(t, append([]string{"report", "./orders", "--traces", traces, "--html=" + file}, args...)...); code != ExitOK {
			t.Fatalf("code=%d %s", code, stderr)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if p := page(); strings.Contains(p, "Classification:") || strings.Contains(p, "Coverage basis") {
		t.Error("the page has classification lines by default")
	}
	p := page("--coverage-basis", "testable")
	for _, want := range []string{`<pre class="classification">`, "Testable coverage:     2 / 3 = 66.7%", "Coverage basis used:   testable"} {
		if !strings.Contains(p, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

// --all ignores the config, so a config that enables classification does
// nothing with it; --coverage-basis still works.
func TestAllIgnoresClassificationConfig(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	enabled := writeConfig(t, `{"classification": {"enabled": true}}`)
	out, _, code := run(t, "report", "./orders", "--traces", traces, "--config", enabled, "--all")
	if code != ExitOK || strings.Contains(out, "Classification:") {
		t.Errorf("--all with an enabled config: code=%d\n%s", code, out)
	}
	out, _, _ = run(t, "report", "./orders", "--traces", traces, "--config", enabled, "--all", "--coverage-basis", "overall")
	if !strings.Contains(out, "Coverage basis used:   overall") {
		t.Errorf("--all with --coverage-basis lost the block:\n%s", out)
	}
}

func TestSummaryWithClassification(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out, stderr, code := run(t, "report", "./orders", "--traces", traces, "--summary", "--coverage-basis", "testable")
	i, j, k := strings.Index(out, "Branches:"), strings.Index(out, "Classification:"), strings.Index(out, "0 workflows excluded")
	if code != ExitOK || stderr != "" || !(0 < i && i < j && j < k) {
		t.Errorf("--summary: block must sit between the Branches line and the excluded line (%d %d %d), code=%d stderr=%q\n%s", i, j, k, code, stderr, out)
	}
	out, stderr, code = run(t, "report", "./orders", "--traces", traces, "--summary", "--json", "--coverage-basis", "testable")
	m := decode(t, out)
	if code != ExitOK || stderr != "pathkit report: --summary ignored because --json was passed.\n" || m["coverageBasis"] != "testable" || m["classificationShown"] != true {
		t.Errorf("--json --summary: code=%d stderr=%q basis=%v shown=%v", code, stderr, m["coverageBasis"], m["classificationShown"])
	}
}

// coverage over several workflows prints the block once, after the
// project total; with --function (one workflow) it prints it too.
func TestCoverageBlockPlacement(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for _, tt := range []struct {
		name string
		args []string
		many bool
	}{{"several workflows", []string{"./..."}, true}, {"--function", []string{"./...", "--function", "orders.OrderWorkflow"}, false}} {
		out, stderr, code := run(t, append([]string{"coverage"}, append(tt.args, "--traces", traces, "--coverage-basis", "testable")...)...)
		if code != ExitOK || stderr != "" && !strings.Contains(stderr, "warning") {
			t.Fatalf("%s: code=%d stderr=%q", tt.name, code, stderr)
		}
		if n := strings.Count(out, "Classification:"); n != 1 {
			t.Errorf("%s: block printed %d times", tt.name, n)
		}
		block, excluded := strings.Index(out, "Classification:"), strings.Index(out, "0 workflows excluded")
		untested := strings.LastIndex(out, "Untested paths:")
		if !(untested < block && block < excluded) {
			t.Errorf("%s: block at %d, last 'Untested paths:' at %d, excluded line at %d", tt.name, block, untested, excluded)
		}
		if tt.many && !strings.Contains(out, " paths total · ") {
			t.Errorf("%s: no project total line", tt.name)
		}
	}
}

// The HTML page names the basis in its --fail-under line, and its overall
// total is not threshold-padded when the testable basis is the one checked.
func TestHTMLFailUnderBasis(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	file := t.TempDir() + "/r.html"
	_, _, code := run(t, "report", "./orders", "--traces", traces, "--html="+file, "--fail-under", "70", "--coverage-basis", "testable")
	data, err := os.ReadFile(file)
	if err != nil || code != ExitBelowThreshold {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(string(data), "is below it (exit code 2) (basis: testable)") {
		t.Errorf("the page's fail-under line has no basis:\n%s", data)
	}
}

// With the testable basis the overall headline gets no threshold-aware
// extra decimals (it is not the number --fail-under compares); with the
// overall basis, or classification off, it keeps them as before.
func TestOverallHeadlineThreshold(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	// 66.67% is within rounding of 66.7: the overall basis shows the extra decimals.
	out, _, _ := run(t, "report", "./orders", "--traces", traces, "--fail-under", "66.7", "--coverage-basis", "overall")
	if !strings.Contains(out, "66.67% project coverage") {
		t.Errorf("overall basis lost the extra decimals:\n%s", out)
	}
	out, _, _ = run(t, "report", "./orders", "--traces", traces, "--fail-under", "66.7", "--coverage-basis", "testable")
	if !strings.Contains(out, "66.7% project coverage") || !strings.Contains(out, "Testable coverage:     2 / 3 = 66.67%") {
		t.Errorf("testable basis: headline should be plain, the testable line precise:\n%s", out)
	}
	out, _, _ = run(t, "report", "./orders", "--traces", traces, "--fail-under", "66.7")
	if !strings.Contains(out, "66.67% project coverage") {
		t.Errorf("classification off must keep the old headline:\n%s", out)
	}
}

// writeUntestableTraces records runs of untestable.Checkout (hand-written
// traces, like ordersTraces) that took the given step lists.
func writeUntestableTraces(t *testing.T, runs ...[]string) string {
	t.Helper()
	res, err := load.Load(absPath(t, "../../testdata/fixtures/untestable"))
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name == "untestable.Checkout" {
			hash = model.FunctionHash(wf.Pkg.Fset, wf.Func)
		}
	}
	dir := t.TempDir()
	for i, steps := range runs {
		writeTrace(t, dir, string(rune('a'+i)), map[string]any{"schemaVersion": 2, "tool": "pathkit-go", "workflow": "untestable.Checkout",
			"functionHash": hash, "status": "complete", "steps": steps})
	}
	return dir
}

// A //pathkit:untestable comment marks the paths through one exit: they
// stay on the map and in overall coverage, and leave testable coverage.
func TestUntestableCommentEndToEnd(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	cfg := writeConfig(t, `{"classification": {"enabled": true}}`)
	args := func(traces string, more ...string) []string {
		return append([]string{"coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg, "--json"}, more...)
	}
	// Checkout's paths: express&&zero true (the marked one), false then large, false then ok.
	traces := writeUntestableTraces(t, []string{"J1.false", "J2.true"}, []string{"J1.false", "J2.false"})
	out, stderr, code := run(t, args(traces)...)
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	m := decode(t, out)
	cl := m["classification"].(map[string]any)
	if cl["testableCoverage"].(map[string]any)["total"] != 2.0 || cl["testableCoverage"].(map[string]any)["covered"] != 2.0 ||
		cl["overallCoverage"].(map[string]any)["total"] != 3.0 || cl["overallCoverage"].(map[string]any)["covered"] != 2.0 {
		t.Errorf("want testable 2/2 and overall 2/3: %v", cl)
	}
	var markedID string
	for _, p := range m["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		p := p.(map[string]any)
		if p["bucket"] == "cannot-be-tested" {
			markedID = p["stableId"].(string)
			if p["label"] != "manual" || p["source"] != "manual" || !strings.Contains(p["explanation"].(string), "//pathkit:untestable at untestable.go:") ||
				!strings.Contains(p["explanation"].(string), "express is never combined with a zero amount") {
				t.Errorf("marked path: %v", p)
			}
		}
	}
	if markedID == "" {
		t.Fatal("no path was marked")
	}
	text, _, _ := run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg)
	for _, want := range []string{"  //pathkit:untestable comments: 1 path marked untestable", "    manual: 1 [auto 0 | manual 1]", "Testable coverage:     2 / 2 = 100.0%"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	// A real run of the marked path wins over the comment: warning, testable.
	both := writeUntestableTraces(t, []string{"J1.true"})
	out, stderr, _ = run(t, args(both)...)
	if !strings.Contains(stderr, "was classified cannot-be-tested") || !strings.Contains(stderr, "//pathkit:untestable at untestable.go:") {
		t.Errorf("stderr %q", stderr)
	}
	if cl := decode(t, out)["classification"].(map[string]any)["buckets"].(map[string]any); cl["cannotBeTested"].(map[string]any)["paths"] != 0.0 {
		t.Errorf("a path a test ran was left untestable: %v", cl)
	}

	// The config wins over the comment: mark_testable puts the path back.
	cfg2 := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true, "paths": {"mark_testable": [{"workflow": "untestable.Checkout", "stable_id": "`+markedID+`", "reason": "PathKit was told wrong"}]}}}`)
	out, stderr, code = run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg2, "--json")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	m = decode(t, out)
	if code != ExitOK || stderr != "" || m["classification"].(map[string]any)["buckets"].(map[string]any)["cannotBeTested"].(map[string]any)["paths"] != 0.0 {
		t.Errorf("config mark_testable should beat the comment: code=%d stderr=%q %v", code, stderr, m["classification"])
	}
	for _, p := range m["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		p := p.(map[string]any)
		if p["stableId"] == markedID && (p["bucket"] != "testable" || p["source"] != "manual" || len(p["extraReasons"].([]any)) != 1) {
			t.Errorf("overridden path: %v", p)
		}
	}
}

// A comment PathKit can't use makes the workflow not analyzable: coverage
// stops and says where, instead of ignoring the comment.
func TestBadUntestableCommentStopsCoverage(t *testing.T) {
	bad := absPath(t, "../../testdata/fixtures/untestablebad")
	traces := t.TempDir()
	writeTrace(t, traces, "a", map[string]any{"schemaVersion": 2, "tool": "pathkit-go", "workflow": "untestablebad.NoReason", "functionHash": "x", "status": "complete", "steps": []string{}})
	_, stderr, code := run(t, "coverage", bad, "--traces", traces)
	if code != ExitError || !strings.Contains(stderr, "in scope but not analyzable: ") ||
		!strings.Contains(stderr, `untestablebad.NoReason: //pathkit:untestable at untestablebad.go:19: it needs a reason, like reason="the carrier never sends this"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

// Config entries, a comment and a rule can all apply to one path. The
// override is the main reason; the comment and then the rule are listed as
// extra reasons, in that order. A custom label works end to end.
func TestAllSourcesOnOnePath(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	traces := writeUntestableTraces(t, []string{"J1.false", "J2.true"})
	pkg := `"packages": [` + jsonString(fixture) + `], `
	runCfg := func(cfg string, more ...string) (string, string, int) {
		return run(t, append([]string{"coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg, "--json"}, more...)...)
	}
	// Find the comment-marked path's id first.
	out, _, _ := runCfg(writeConfig(t, `{`+pkg+`"classification": {"enabled": true}}`))
	var id string
	for _, p := range decode(t, out)["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		if p.(map[string]any)["bucket"] == "cannot-be-tested" {
			id = p.(map[string]any)["stableId"].(string)
		}
	}
	if id == "" {
		t.Fatal("the comment marked nothing")
	}
	cfg := writeConfig(t, `{`+pkg+`"classification": {"enabled": true,
	  "custom_labels": [{"name": "my-label", "definition": "something only this project knows", "bucket": "cannot-be-tested"}],
	  "paths": {"mark_untestable": [{"workflow": "Checkout", "stable_id": "`+id+`", "label": "my-label", "reason": "the override"}]},
	  "rules": [{"id": "r1", "workflow": "Checkout", "label": "set-then-test", "when": ["if express && amount == 0 --true"], "impossible": true, "reason": "the rule"}]}}`)
	out, stderr, code := runCfg(cfg)
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, p := range decode(t, out)["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		p := p.(map[string]any)
		if p["stableId"] != id {
			continue
		}
		extra := p["extraReasons"].([]any)
		if p["label"] != "my-label" || p["bucket"] != "cannot-be-tested" || !strings.Contains(p["explanation"].(string), "the override") ||
			len(extra) != 2 || !strings.HasPrefix(extra[0].(string), "//pathkit:untestable at untestable.go:") || extra[1] != `rule "r1"` {
			t.Errorf("path: %v", p)
		}
	}
}

// mark_testable alone: nothing becomes untestable, and the note says one
// path was marked testable.
func TestMarkTestableAlone(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	traces := writeUntestableTraces(t, []string{"J1.false", "J2.true"})
	pkg := `"packages": [` + jsonString(fixture) + `], `
	probe, _, _ := run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--json", "--config", writeConfig(t, `{`+pkg+`"classification": {"enabled": true}}`))
	var id string
	for _, p := range decode(t, probe)["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		if p.(map[string]any)["covered"] == false && p.(map[string]any)["bucket"] == "testable" {
			id = p.(map[string]any)["stableId"].(string)
		}
	}
	cfg := writeConfig(t, `{`+pkg+`"classification": {"enabled": true, "paths": {"mark_testable": [{"workflow": "untestable.Checkout", "stable_id": "`+id+`", "reason": "it can run"}]}}}`)
	out, stderr, code := run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg)
	if code != ExitOK || stderr != "" || !strings.Contains(out, "Config overrides: 0 paths marked untestable, 1 path marked testable") {
		t.Errorf("code=%d stderr=%q\n%s", code, stderr, out)
	}
}

// With --function, the notes (and the stale check) concern only the picked
// workflow; entries for other workflows are not mentioned.
func TestFunctionFiltersNotesAndStale(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	traces := writeUntestableTraces(t, []string{"J1.false", "J2.true"})
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true,
	  "paths": {"mark_untestable": [{"workflow": "SameLine", "stable_id": "s1:ffffffffffff", "reason": "r"}]},
	  "rules": [{"id": "elsewhere", "workflow": "SameLine", "label": "set-then-test", "when": ["if nobody --true"], "impossible": true, "reason": "r"}]}}`)
	out, stderr, code := run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg)
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q (entries for a workflow that was not picked must not be reported)", code, stderr)
	}
	if strings.Contains(out, "Config overrides") || strings.Contains(out, "elsewhere") {
		t.Errorf("notes mention another workflow's entries:\n%s", out)
	}
	// Picking that workflow instead reports both as stale (neither matches).
	_, stderr, _ = run(t, "coverage", fixture, "--function", "untestable.SameLine", "--traces", writeUntestableTraces(t, []string{"J1.false"}), "--config", cfg)
	if !strings.Contains(stderr, "stale entry") || !strings.Contains(stderr, `stale rule "elsewhere"`) {
		t.Errorf("stderr %q", stderr)
	}
}

// With classification off, a good comment is ignored with a warning, and a
// bad one still stops (it would otherwise hide a decision someone made).
func TestCommentsWithClassificationOff(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	traces := writeUntestableTraces(t, []string{"J1.false", "J2.true"})
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`]}`)
	out, stderr, code := run(t, "coverage", fixture, "--function", "untestable.Checkout", "--traces", traces, "--config", cfg, "--json")
	if code != ExitOK || !strings.Contains(stderr, "untestable.Checkout has //pathkit:untestable comments, but classification is not enabled, so they are ignored") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	if decode(t, out)["classification"].(map[string]any)["buckets"].(map[string]any)["cannotBeTested"].(map[string]any)["paths"] != 0.0 {
		t.Error("a comment was applied although classification is off")
	}
	// analyze lists every workflow of the package as not analyzable (they
	// all have bad comments), so it has nothing to show and exits 1.
	stdout, stderr, code := run(t, "analyze", absPath(t, "../../testdata/fixtures/untestablebad"))
	if code != ExitError || !strings.Contains(stdout+stderr, "in scope but not analyzable: untestablebad.NoReason: //pathkit:untestable at untestablebad.go:") {
		t.Errorf("analyze of a bad comment: code=%d\n%s%s", code, stdout, stderr)
	}
}

// writeTracesFor records hand-written runs of one workflow in a fixture package.
func writeTracesFor(t *testing.T, folder, workflow string, runs ...[]string) string {
	t.Helper()
	res, err := load.Load(absPath(t, folder))
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	for _, wf := range discover.All(res.Packages) {
		if wf.Workflow.Name == workflow {
			g, err := model.Build(wf.Workflow)
			if err != nil {
				t.Fatal(err)
			}
			hash = g.CodeHash()
		}
	}
	if hash == "" {
		t.Fatalf("%s not found", workflow)
	}
	dir := t.TempDir()
	for i, steps := range runs {
		writeTrace(t, dir, string(rune('a'+i)), map[string]any{"schemaVersion": 2, "tool": "pathkit-go", "workflow": workflow,
			"functionHash": hash, "status": "complete", "steps": steps})
	}
	return dir
}

// The path cap is per workflow (2000 listed paths each). In a workflow with
// more paths than that, an entry that matches none of the listed paths is
// reported as "beyond the path cap", not "stale"; in a workflow under the
// cap the same entry is stale.
func TestBeyondThePathCapIsNotStale(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/rules")
	allTrue := []string{}
	for i := 1; i <= 12; i++ {
		allTrue = append(allTrue, "J"+strconvI(i)+".true")
	}
	traces := writeTracesFor(t, "../../testdata/fixtures/rules", "rules.TwelveIfs", allTrue)
	entry := `{"workflow": "rules.TwelveIfs", "stable_id": "s1:ffffffffffff", "reason": "a path somewhere"}`
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "workflows": {"include": ["TwelveIfs"]},
	  "classification": {"enabled": true, "paths": {"mark_untestable": [`+entry+`]}}}`)
	out, stderr, code := run(t, "coverage", fixture, "--traces", traces, "--config", cfg, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "rules.TwelveIfs s1:ffffffffffff (a path somewhere) matches none of the listed paths") ||
		!strings.Contains(stderr, "beyond the path cap") || strings.Contains(stderr, "stale") {
		t.Errorf("stderr %q", stderr)
	}
	m := decode(t, out)
	if w := m["workflows"].([]any)[0].(map[string]any); w["truncated"] != true || w["paths"].(map[string]any)["total"] != 2000.0 {
		t.Errorf("TwelveIfs should list exactly 2000 paths and be truncated: %v", w["paths"])
	}
}

func strconvI(i int) string { return strconv.Itoa(i) }

// A misspelled detector name is reported whenever the config is loaded,
// even with classification switched off and for commands that do not
// classify at all.
func TestDetectorNameCheckedWithoutClassification(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".pathkitrc.json")
	body := `{"classification": {"enabled": false, "detectors": {"disable": [{"name": "closed-signal-chanel", "reason": "typo"}]}}}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"analyze", "../../testdata/pilot/orders", "--config", cfg},
		{"clean", "--config", cfg},
	} {
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != 1 || !strings.Contains(errb.String(), `"closed-signal-chanel" is not a detector (detectors: closed-signal-channel,`) {
			t.Errorf("%v: exit %d, stderr %q", args, code, errb.String())
		}
	}
}
