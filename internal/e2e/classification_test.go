package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonText(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// On the real pilot: the two manual override examples (billing's
// and polling's skipped loops, reason "input count is never 0"), and every
// way a classification entry can be wrong. The committed pilot, baseline
// and goldens are not touched: the config lives under testdata/scopes.
func TestClassificationOnPilot(t *testing.T) {
	pilot := abs(t, "../../testdata/pilot")
	pair := abs(t, "../../testdata/scopes/classified-pair/.pathkitrc.json")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", pilot+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}

	type result struct {
		stdout, stderr string
		code           int
		json           map[string]any
	}
	runJSON := func(t *testing.T, cmd string, args ...string) result {
		t.Helper()
		out, errOut, code := pathkit(t, append([]string{cmd, pilot + "/...", "--traces", traces, "--json"}, args...)...)
		r := result{stdout: out, stderr: errOut, code: code}
		if strings.HasPrefix(out, "{") {
			if err := json.Unmarshal([]byte(out), &r.json); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	count := func(m map[string]any, path ...string) (total, covered float64, percent float64) {
		for _, k := range path {
			m = m[k].(map[string]any)
		}
		return m["total"].(float64), m["covered"].(float64), m["percent"].(float64)
	}
	bucket := func(r result, key string) float64 {
		return r.json["classification"].(map[string]any)["buckets"].(map[string]any)[key].(map[string]any)["paths"].(float64)
	}
	paths := func(r result) map[string]map[string]any { // "workflow stableId" -> path
		out := map[string]map[string]any{}
		for _, w := range r.json["workflows"].([]any) {
			w := w.(map[string]any)
			for _, p := range w["pathList"].([]any) {
				p := p.(map[string]any)
				out[w["name"].(string)+" "+p["stableId"].(string)] = p
			}
		}
		return out
	}
	// writeCfg writes a config whose classification section is cl, next to
	// optional extra top-level JSON.
	writeCfg := func(t *testing.T, cl string, extra string) string {
		t.Helper()
		file := filepath.Join(t.TempDir(), ".pathkitrc.json")
		body := `{"packages": [` + jsonText(t, pilot+"/...") + `], ` + extra + `"classification": ` + cl + `}`
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return file
	}
	entry := func(workflow, id, reason string) string {
		return `{"workflow": ` + jsonText(t, workflow) + `, "stable_id": ` + jsonText(t, id) + `, "reason": ` + jsonText(t, reason) + `}`
	}
	const billing, polling = "billing.SubscriptionWorkflow", "polling.ReportPollingWorkflow"
	const billingID, pollingID = "s1:59aa014811bd", "s1:c048fc889a4c"

	t.Run("the pair: 20/36 = 55.6% testable, 20/38 = 52.6% overall", func(t *testing.T) {
		r := runJSON(t, "report", "--config", pair)
		if r.code != 0 || r.stderr != "" {
			t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
		}
		if tt, c, p := count(r.json, "classification", "testableCoverage"); tt != 36 || c != 20 || p != 55.6 {
			t.Errorf("testable coverage %v/%v = %v%%, want 20/36 = 55.6%%", c, tt, p)
		}
		if tt, c, p := count(r.json, "classification", "overallCoverage"); tt != 38 || c != 20 || p != 52.6 {
			t.Errorf("overall coverage %v/%v = %v%%, want 20/38 = 52.6%%", c, tt, p)
		}
		if tt, c, p := count(r.json, "total"); tt != 38 || c != 20 || p != 52.6 {
			t.Errorf("total %v/%v = %v%%", c, tt, p)
		}
		if bucket(r, "testable") != 36 || bucket(r, "cannotBeTested") != 2 || bucket(r, "needsReview") != 0 || bucket(r, "needsSpecialTest") != 0 {
			t.Errorf("buckets %v", r.json["classification"])
		}
		if r.json["coverageBasis"] != "testable" || r.json["classificationShown"] != true {
			t.Errorf("basis %v shown %v", r.json["coverageBasis"], r.json["classificationShown"])
		}
		marked := 0
		for key, p := range paths(r) {
			if key == billing+" "+billingID || key == polling+" "+pollingID {
				marked++
				if p["bucket"] != "cannot-be-tested" || p["label"] != "manual" || p["source"] != "manual" || p["confidence"] != "certain" ||
					p["covered"] != false || !strings.Contains(p["explanation"].(string), "input count is never 0") {
					t.Errorf("%s: %v", key, p)
				}
			} else if p["bucket"] != "testable" {
				t.Errorf("%s should stay testable: %v", key, p)
			}
		}
		if marked != 2 {
			t.Errorf("%d of the two paths found", marked)
		}

		text, stderr, code := pathkit(t, "report", pilot+"/...", "--traces", traces, "--config", pair)
		if code != 0 || stderr != "" {
			t.Fatalf("text: exit %d %q", code, stderr)
		}
		for _, want := range []string{
			"38 paths total · 20 covered · 18 missed · 52.6% project coverage",
			"  Testable:            36",
			"  Cannot be tested:    2\n    manual: 2 [auto 0 | manual 2]\n",
			"  Config overrides: 2 paths marked untestable, 0 paths marked testable",
			"Testable coverage:     20 / 36 = 55.6%  (testable paths only)",
			"Overall coverage:      20 / 38 = 52.6%  (all paths)",
			"Coverage basis used:   testable",
			" [cannot-be-tested: manual · manual · config override]",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("report text lacks %q:\n%s", want, text)
			}
		}
	})

	t.Run("--fail-under checks the chosen basis", func(t *testing.T) {
		r := runJSON(t, "report", "--config", pair, "--fail-under", "55")
		if r.code != 0 {
			t.Errorf("testable basis: 55.6 >= 55 should pass, exit %d %q", r.code, r.stderr)
		}
		r = runJSON(t, "report", "--config", pair, "--fail-under", "55", "--coverage-basis", "overall")
		if r.code != 2 || !strings.Contains(r.stderr, "coverage 52.6% is below --fail-under 55% (basis: overall)") {
			t.Errorf("overall basis: 52.6 < 55 should fail with exit 2, got %d %q", r.code, r.stderr)
		}
	})

	t.Run("marking a covered path: a warning, and it stays testable", func(t *testing.T) {
		base := runJSON(t, "report")
		var coveredID string
		for key, p := range paths(base) {
			if strings.HasPrefix(key, "orders.OrderWorkflow ") && p["covered"] == true {
				coveredID = p["stableId"].(string)
			}
		}
		if coveredID == "" {
			t.Fatal("no covered orders path")
		}
		cfg := writeCfg(t, `{"enabled": true, "paths": {"mark_untestable": [`+entry("orders.OrderWorkflow", coveredID, "wrongly marked")+`]}}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 0 || !strings.Contains(r.stderr, "was classified cannot-be-tested") || !strings.Contains(r.stderr, "wrongly marked") ||
			!strings.Contains(r.stderr, "counting it as testable") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
		p := paths(r)["orders.OrderWorkflow "+coveredID]
		if p["bucket"] != "testable" || p["covered"] != true {
			t.Errorf("the covered path: %v", p)
		}
		if bucket(r, "cannotBeTested") != 0 {
			t.Errorf("nothing may be cannot-be-tested: %v", r.json["classification"])
		}
		if tt, c, _ := count(r.json, "classification", "testableCoverage"); tt != 38 || c != 20 {
			t.Errorf("testable coverage %v/%v, want 20/38", c, tt)
		}
	})

	t.Run("an unknown stableId is a stale warning, and changes nothing", func(t *testing.T) {
		cfg := writeCfg(t, `{"enabled": true, "paths": {"mark_untestable": [`+entry(billing, "s1:ffffffffffff", "a typo")+`]}}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 0 || !strings.Contains(r.stderr, "stale entry: "+billing+" s1:ffffffffffff (a typo) matches no path") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
		if bucket(r, "cannotBeTested") != 0 {
			t.Errorf("%v", r.json["classification"])
		}
	})

	t.Run("a missing reason is a load error", func(t *testing.T) {
		cfg := writeCfg(t, `{"enabled": true, "paths": {"mark_untestable": [{"workflow": "`+billing+`", "stable_id": "`+billingID+`"}]}}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, `pathkit report: `+cfg+`: classification.paths.mark_untestable[0] (`+billing+` `+billingID+`) needs a "reason"`) {
			t.Errorf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
		}
	})

	t.Run("an excluded workflow is gone from the totals, and its entry is not stale", func(t *testing.T) {
		r := runJSON(t, "report", "--config", pair, "--exclude", "billing.SubscriptionWorkflow")
		if r.code != 0 || r.stderr != "" {
			t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
		}
		for _, w := range r.json["workflows"].([]any) {
			if w.(map[string]any)["name"] == billing {
				t.Error("billing is still in the report")
			}
		}
		// Billing had 3 paths, 2 covered. Only polling's entry applies now.
		if tt, c, _ := count(r.json, "total"); tt != 35 || c != 18 {
			t.Errorf("total %v/%v, want 18/35", c, tt)
		}
		if tt, c, _ := count(r.json, "classification", "testableCoverage"); tt != 34 || c != 18 {
			t.Errorf("testable coverage %v/%v, want 18/34", c, tt)
		}
		if bucket(r, "cannotBeTested") != 1 {
			t.Errorf("%v", r.json["classification"])
		}
		if ex := r.json["excluded"].([]any); len(ex) != 1 || ex[0].(map[string]any)["name"] != billing {
			t.Errorf("excluded: %v", ex)
		}
	})

	t.Run("a workflow that does not exist is an error with a hint", func(t *testing.T) {
		cfg := writeCfg(t, `{"enabled": true, "paths": {"mark_untestable": [`+entry("billing.SubscriptionWorkflw", billingID, "r")+`]}}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 1 || !strings.Contains(r.stderr, `classification.paths.mark_untestable (`+billingID+`) workflow "billing.SubscriptionWorkflw" matches no workflow`) ||
			!strings.Contains(r.stderr, "did you mean") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})

	t.Run("entries are ignored, with a warning, when classification is not enabled", func(t *testing.T) {
		cfg := writeCfg(t, `{"enabled": false, "paths": {"mark_untestable": [`+entry(billing, billingID, "r")+`]}}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 0 || !strings.Contains(r.stderr, "classification is not enabled, so the overrides, rules and custom labels in "+cfg+" are ignored") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
		if bucket(r, "cannotBeTested") != 0 || r.json["classificationShown"] != false {
			t.Errorf("%v", r.json["classification"])
		}
	})

	t.Run("a rule removes the paths that take all its decisions", func(t *testing.T) {
		rule := `{"id": "polling-no-poll", "workflow": "polling.ReportPollingWorkflow", "label": "loop-minimum",
		  "when": ["for attempt <= in.MaxPolls --exit"], "impossible": true, "reason": "MaxPolls is always at least 1"}`
		cfg := writeCfg(t, `{"enabled": true, "rules": [`+rule+`]}`, "")
		r := runJSON(t, "report", "--config", cfg)
		if r.code != 0 {
			t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
		}
		// Polling's paths that leave the loop through "exit": "not entered"
		// (never run, so marked) and "pending, then gave up" (a real test
		// ran it, so the rule warns and it stays testable).
		marked := 0
		for key, p := range paths(r) {
			if strings.HasPrefix(key, polling+" ") && p["ruleId"] == "polling-no-poll" {
				marked++
				if p["bucket"] != "cannot-be-tested" || p["label"] != "loop-minimum" || p["source"] != "manual" || p["covered"] == true {
					t.Errorf("%v", p)
				}
			}
		}
		if marked != 1 || bucket(r, "cannotBeTested") != 1 {
			t.Errorf("%d paths marked by the rule; cannot-be-tested = %v; want 1 and 1", marked, bucket(r, "cannotBeTested"))
		}
		if n := strings.Count(r.stderr, "counting it as testable"); n != 1 || !strings.Contains(r.stderr, `Rule "polling-no-poll"`) {
			t.Errorf("want one conflict warning naming the rule, got %q", r.stderr)
		}
		text, _, _ := pathkit(t, "report", pilot+"/...", "--traces", traces, "--config", cfg)
		const tag = ` [cannot-be-tested: loop-minimum · manual · rule "polling-no-poll"]`
		for _, want := range []string{"    loop-minimum: 1 [auto 0 | manual 1]", `  Rule "polling-no-poll" (polling.ReportPollingWorkflow): 1 path marked untestable`, tag} {
			if !strings.Contains(text, want) {
				t.Errorf("report text lacks %q:\n%s", want, text)
			}
		}
		// The rule id sits right after the one path it marked, and only that one.
		if n := strings.Count(text, tag); n != 1 {
			t.Errorf("the tag appears %d times, want 1", n)
		}
		covText, _, _ := pathkit(t, "coverage", pilot+"/polling", "--traces", traces, "--config", cfg)
		if strings.Count(covText, tag) != 1 {
			t.Errorf("coverage text should tag the path once:\n%s", covText)
		}
		htmlFile := filepath.Join(t.TempDir(), "r.html")
		if _, stderr, code := pathkit(t, "report", pilot+"/...", "--traces", traces, "--config", cfg, "--html="+htmlFile); code != 0 {
			t.Fatalf("html: %d %s", code, stderr)
		}
		page, _ := os.ReadFile(htmlFile)
		// The path's row (in the page's data) carries the label, the source
		// and the rule id; the label has a definition in the page.
		var found pageRow
		for _, w := range pageWorkflows(t, string(page)) {
			for _, r := range w.Rows {
				if r.Label == "loop-minimum" {
					found = r
				}
			}
		}
		if found.Bucket != "cannot-be-tested" || found.By != `manual · rule "polling-no-poll"` {
			t.Errorf("the page's row for the rule's path: %+v", found)
		}
		for _, want := range []string{
			`<section class="definitions" id="definitions"`,
			`<article class="def" id="def-loop-minimum"`,
		} {
			if !strings.Contains(string(page), want) {
				t.Errorf("the HTML page lacks %q", want)
			}
		}
	})
}
