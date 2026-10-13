package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ordersTracesWith adds hand-written orders runs (with the current hash)
// to the two of ordersTraces.
func ordersTracesWith(t *testing.T, runs map[string][]string) string {
	t.Helper()
	dir := ordersTraces(t)
	raw, err := os.ReadFile(filepath.Join(dir, "a.trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, steps := range runs {
		var tr map[string]any
		if err := json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(err)
		}
		tr["steps"] = steps
		writeTrace(t, dir, name, tr)
	}
	return dir
}

func TestUnmatchedReport(t *testing.T) {
	traces := ordersTracesWith(t, map[string][]string{
		"early":   {"J1.false"},              // stops before ChargeCard decides
		"early2":  {"J1.false"},              // the same steps: one group of 2
		"after":   {"J1.true", "J2.success"}, // goes on after the end
		"nowhere": {"J9.true"},               // not on the map
		"skipped": {"J2.success"},            // skips the first decision
	})
	inPilot(t)
	plain, _, _ := run(t, "report", "./orders", "--traces", traces)
	out, _, code := run(t, "report", "./orders", "--traces", traces, "--unmatched")
	if code != ExitOK || !strings.HasPrefix(out, plain) {
		t.Fatalf("--unmatched must only add a section: code=%d\n%s", code, out)
	}
	section := strings.TrimPrefix(out, plain)
	for _, want := range []string{
		"\nTraces that fit no path: 4 groups, 5 trace files\n",
		"  orders.OrderWorkflow, steps id t1:",
		", 2 trace files, not acknowledged\n    recorded steps:    J1.false\n" +
			"    followed the map:  Start -> if in.AmountCents <= 0 --false-->\n" +
			"    then:              the trace stops at J2 (ChargeCard (activity)) before reaching an end\n" +
			"    the map expected:  ChargeCard (activity) --failure (J2.failure)\n" +
			"                       ChargeCard (activity) --success (J2.success)\n" +
			"    likely: the run ended (returned or failed) earlier than the map allows; PathKit assumes an `if err != nil`",
		"    recorded steps:    J1.true J2.success\n    followed the map:  Start -> if in.AmountCents <= 0 --true-->\n",
		"    the map expected:  nothing more: the map says the run ends here, at End (completed)\n    likely: the map says the run had ended, but it went on deciding",
		"    recorded steps:    J9.true\n    followed the map:  Start\n",
		"    likely: the step is not on this workflow's map at all",
		"    recorded steps:    J2.success\n    followed the map:  Start\n",
		"    likely: the run reached `ChargeCard (activity)` without the map's next decision `if in.AmountCents <= 0`",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("missing %q in\n%s", want, section)
		}
	}

	// coverage too, and the JSON.
	cov, _, _ := run(t, "coverage", "./orders/orders.go", "--traces", traces, "--unmatched")
	if !strings.Contains(cov, "\nTraces that fit no path: 4 groups, 5 trace files\n") {
		t.Errorf("coverage --unmatched:\n%s", cov)
	}
	js, _, _ := run(t, "report", "./orders", "--traces", traces, "--json", "--unmatched")
	list := decode(t, js)["unmatchedTraces"].([]any)
	if len(list) != 4 {
		t.Fatalf("unmatchedTraces: %v", list)
	}
	first := list[0].(map[string]any)
	if first["count"] != 2.0 || first["followed"] != "Start -> if in.AmountCents <= 0 --false-->" || len(first["expected"].([]any)) != 2 || !strings.HasPrefix(first["likelyCause"].(string), "likely: ") {
		t.Errorf("first group: %v", first)
	}
	js, _, _ = run(t, "report", "./orders", "--traces", traces, "--json")
	if strings.Contains(js, "unmatchedTraces") {
		t.Error("unmatchedTraces without --unmatched")
	}

	// Every trace fits: the section says so.
	clean := ordersTraces(t)
	out, _, _ = run(t, "report", "./orders", "--traces", clean, "--unmatched")
	if !strings.HasSuffix(out, "\nTraces that fit no path: none\n") {
		t.Errorf("none:\n%s", out)
	}
	js, _, _ = run(t, "report", "./orders", "--traces", clean, "--json", "--unmatched")
	if !strings.Contains(js, `"unmatchedTraces": []`) {
		t.Errorf("an empty list expected:\n%s", js)
	}
}
