package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// stableIDsOf lists one workflow's stable ids in path order, from the model.
func stableIDsOf(t *testing.T, folder, workflow string) []string {
	t.Helper()
	res, err := load.Load(absPath(t, folder))
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name != workflow {
			continue
		}
		g, err := model.Build(wf)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			ids = append(ids, p.StableID())
		}
		return ids
	}
	t.Fatalf("%s not found in %s", workflow, folder)
	return nil
}

func TestExplainDefault(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	ids := stableIDsOf(t, pilot+"/orders", "orders.OrderWorkflow")
	out, stderr, code := run(t, "explain", ids[2], "./orders", "--traces", traces)
	want := ids[2] + " is path 3 of orders.OrderWorkflow (orders/orders.go)\n" +
		"  Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)\n\n" +
		"Tested:      no; no recorded run took this path\n" +
		"Bucket:      testable: A normal test can make the code take this path.\n" +
		"Label:       none (an ordinary path)\n" +
		"Decided by:  nobody: no detector, rule, override or comment applies, so the path is testable by default\n" +
		"Why:         No detector or rule says this path is impossible, so a normal test is expected to be able to run it.\n" +
		"Note:        classification is not enabled, so PathKit does not look for impossible paths and every path counts as testable (set \"enabled\": true under \"classification\" in .pathkitrc.json)\n" +
		"To cover it:\n" +
		"  - make `in.AmountCents <= 0` false\n" +
		"  - let activity ChargeCard succeed (mock it with env.OnActivity, passing the same activity the workflow starts (ChargeCard), with .Return(...); or let it run)\n" +
		"  - expect the workflow to complete\n"
	if code != ExitOK || stderr != "" || out != want {
		t.Fatalf("code=%d stderr=%q\n%s\nwant\n%s", code, stderr, out, want)
	}
	out, _, _ = run(t, "explain", ids[0], "./orders", "--traces", traces)
	if !strings.Contains(out, "Tested:      yes, by 1 recorded run\n") {
		t.Errorf("covered path:\n%s", out)
	}
	// Traces are optional.
	out, _, code = run(t, "explain", ids[0], "./orders", "--traces", t.TempDir())
	if code != ExitOK || !strings.Contains(out, "Tested:      unknown: no trace files in ") {
		t.Errorf("no traces: code=%d\n%s", code, out)
	}
}

func TestExplainErrors(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"explain"}, "pathkit explain: missing <stableId> argument; usage: pathkit explain <stableId> [folder] (the ids are listed by \"pathkit report --json\", field stableId)\n"},
		{[]string{"explain", "3"}, `pathkit explain: "3" is not a stable id (expected "s1:" and 12 hex digits`},
		{[]string{"explain", "s1:000000000000", "./orders", "--traces", traces}, "pathkit explain: no listed path has the stable id s1:000000000000 in 1 measured workflow; "},
		{[]string{"explain", "s1:000000000000", "./orders", "x", "--traces", traces}, "expected a <stableId> and at most one folder, got 3 arguments"},
	} {
		_, stderr, code := run(t, c.args...)
		if code != ExitError || !strings.HasPrefix(stderr, c.want) && !strings.Contains(stderr, c.want) {
			t.Errorf("%v: code=%d stderr=%q, want %q", c.args, code, stderr, c.want)
		}
	}
}

// Each kind of decider is named in plain words.
func TestExplainDecidedBy(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	ids := stableIDsOf(t, pilot+"/orders", "orders.OrderWorkflow")

	// A config rule that claims a path a test ran: testable, and the claim
	// is shown as wrong.
	cfg := ciConfig(t, `{"enabled": true, "rules": [{"id": "never-fails", "workflow": "OrderWorkflow", "label": "contradictory",
	  "when": ["ChargeCard (activity) --failure"], "impossible": true, "reason": "made up for the test"}]}`)
	out, _, code := run(t, "explain", ids[1], "--traces", traces, "--config", cfg)
	if code != ExitOK || !strings.Contains(out, "Bucket:      testable") ||
		!strings.Contains(out, "Decided by:  a test: a recorded run took this path, which proves it can happen, so it counts as testable whatever was claimed\n") ||
		!strings.Contains(out, `Note:        a person: config rule "never-fails" in `+cfg+` said cannot-be-tested (Rule "never-fails": made up for the test), but a test ran this path, so that claim is wrong and should be fixed or removed`) {
		t.Errorf("rule on a covered path:\n%s", out)
	}
	// The CI check is not explain's business; a baseline
	// in the config that does not exist yet must not stop it.
	withCI := ciConfig(t, `{"enabled": true, "ci": {"baseline": "missing.json", "fail_under_testable": 100}}`)
	if _, stderr, code := run(t, "explain", ids[2], "--traces", traces, "--config", withCI); code != ExitOK {
		t.Errorf("CI settings: code=%d stderr=%q", code, stderr)
	}
	// A typo in --function with no folder named the folder as "".
	_, stderr, code := run(t, "explain", ids[2], "--traces", traces, "--config", withCI, "--function", "OrderWorkflw")
	if code != ExitError || strings.Contains(stderr, " in ;") || !strings.Contains(stderr, `did you mean "orders.OrderWorkflow"?`) && !strings.Contains(stderr, `did you mean "OrderWorkflow"?`) {
		t.Errorf("--function typo: code=%d stderr=%q", code, stderr)
	}
	// A mark_testable override and a rule disagree; "Also"
	// must not say they say the same.
	both := ciConfig(t, `{"enabled": true,
	  "paths": {"mark_testable": [{"workflow": "OrderWorkflow", "stable_id": "`+ids[2]+`", "reason": "it can run"}]},
	  "rules": [{"id": "never-charged", "workflow": "OrderWorkflow", "label": "contradictory", "when": ["ChargeCard (activity) --success"], "impossible": true, "reason": "r"}]}`)
	out, _, _ = run(t, "explain", ids[2], "--traces", traces, "--config", both)
	if !strings.Contains(out, `Also:        rule "never-charged" also applied to this path; the first reason found wins (config override, then comment, then rule, then detector)`) || strings.Contains(out, "say the same") {
		t.Errorf("disagreeing reasons:\n%s", out)
	}
	// The classification-off note names the config in use.
	off := ciConfig(t, `{"enabled": false}`)
	out, _, _ = run(t, "explain", ids[2], "--traces", traces, "--config", off)
	if !strings.Contains(out, `under "classification" in `+off+")") {
		t.Errorf("off note:\n%s", out)
	}
	// The same rule on a path no test ran: it decides.
	cfg = ciConfig(t, `{"enabled": true, "rules": [{"id": "never-charged", "workflow": "OrderWorkflow", "label": "contradictory",
	  "when": ["ChargeCard (activity) --success"], "impossible": true, "reason": "made up for the test"}]}`)
	out, _, _ = run(t, "explain", ids[2], "--traces", traces, "--config", cfg)
	for _, want := range []string{
		"Bucket:      cannot-be-tested: ",
		"Label:       contradictory: ",
		`Decided by:  a person: config rule "never-charged" in ` + cfg + "\n",
		`Why:         Rule "never-charged": made up for the test` + "\n",
		"What to do:  ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rule: missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Note:") {
		t.Errorf("no note expected:\n%s", out)
	}

	// A config override (the classified-pair fixture).
	pair := absPath(t, "../scopes/classified-pair/.pathkitrc.json")
	out, _, code = run(t, "explain", "s1:59aa014811bd", "--traces", traces, "--config", pair)
	if code != ExitOK || !strings.Contains(out, "of billing.SubscriptionWorkflow") ||
		!strings.Contains(out, "Decided by:  a person: classification.paths.mark_untestable in "+pair+"\n") ||
		!strings.Contains(out, "Why:         Marked untestable in the config (manual): input count is never 0\n") {
		t.Errorf("override: code=%d\n%s", code, out)
	}
}

func TestExplainDetector(t *testing.T) {
	folder := absPath(t, "../../testdata/fixtures/closedchannel")
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	ids := stableIDsOf(t, folder, "closedchannel.CatchReceive")
	var explained []string
	for _, id := range ids {
		out, stderr, code := run(t, "explain", id, "--config", cfg, "--function", "CatchReceive", "--traces", t.TempDir())
		if code != ExitOK {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		explained = append(explained, out)
	}
	found := false
	for _, out := range explained {
		if strings.Contains(out, "Decided by:  detector closed-signal-channel (automatic, certain)\n") && strings.Contains(out, "Label:       closed-signal-channel: ") {
			found = true
		}
	}
	if !found {
		t.Errorf("no path of CatchReceive was explained as decided by the detector:\n%s", strings.Join(explained, "\n----\n"))
	}
}

// A needs-special-test claim on a path a test ran is
// what the label asks for, not a wrong claim.
func TestExplainSpecialTestRun(t *testing.T) {
	folder := absPath(t, "../../testdata/fixtures/historysuggest")
	res, err := load.Load(folder)
	if err != nil {
		t.Fatal(err)
	}
	var runs [][]string
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name != "historysuggest.CatchDirect" {
			continue
		}
		g, err := model.Build(wf)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			var steps []string
			for _, s := range p.Steps {
				steps = append(steps, s.Exit.ID.String())
			}
			runs = append(runs, steps)
		}
	}
	traces := writeTracesFor(t, "../../testdata/fixtures/historysuggest", "historysuggest.CatchDirect", runs...)
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	seen := false
	for _, id := range stableIDsOf(t, folder, "historysuggest.CatchDirect") {
		out, stderr, code := run(t, "explain", id, "--config", cfg, "--function", "CatchDirect", "--traces", traces)
		if code != ExitOK {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if strings.Contains(out, "said needs-special-test") {
			seen = true
			if strings.Contains(out, "wrong") || !strings.Contains(out, "which is what that asks for, so it counts as testable") {
				t.Errorf("needs-special-test note:\n%s", out)
			}
		}
	}
	if !seen {
		t.Error("no path of CatchDirect was claimed needs-special-test")
	}
}

// The file shown in the first line uses forward slashes on every system, as
// the JSON does. The input is in the system's own form (backslashes on
// Windows), so this is a real check there and a no-op check elsewhere.
func TestExplainShowsFileWithForwardSlashes(t *testing.T) {
	w := coverage.WorkflowResult{Name: "orders.OrderWorkflow", File: filepath.FromSlash("orders/orders.go")}
	p := coverage.PathResult{Number: 1, StableID: "s1:000000000000"}
	got := explainHeader(w, p)
	if want := "s1:000000000000 is path 1 of orders.OrderWorkflow (orders/orders.go)\n"; got != want {
		t.Errorf("first line %q, want %q", got, want)
	}
}

// explain prints the warnings of the workflow that owns the path, not those
// of the other workflows it measured on the way.
func TestExplainPrintsOnlyItsOwnWorkflowsWarnings(t *testing.T) {
	traces := ordersTraces(t)
	writeTrace(t, traces, "billing-old", map[string]any{"schemaVersion": 2, "tool": "pathkit-go", "workflow": "billing.SubscriptionWorkflow",
		"functionHash": "0000000000000000", "status": "complete", "steps": []string{"J1.true"}})
	inPilot(t)
	ids := stableIDsOf(t, pilot+"/orders", "orders.OrderWorkflow")
	_, stderr, _ := run(t, "report", "./...", "--traces", traces)
	if !strings.Contains(stderr, "billing.SubscriptionWorkflow") {
		t.Fatalf("report should warn about the billing trace, got %q", stderr)
	}
	_, stderr, code := run(t, "explain", ids[0], "./...", "--traces", traces)
	if code != ExitOK || strings.Contains(stderr, "billing.SubscriptionWorkflow") {
		t.Errorf("explain of an orders path printed a billing warning: code=%d %q", code, stderr)
	}
	billing := stableIDsOf(t, pilot+"/billing", "billing.SubscriptionWorkflow")
	_, stderr, _ = run(t, "explain", billing[0], "./...", "--traces", traces)
	if !strings.Contains(stderr, "billing.SubscriptionWorkflow") {
		t.Errorf("explain of a billing path lost its own warning: %q", stderr)
	}
}
