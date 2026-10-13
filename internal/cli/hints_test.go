package cli

import (
	"strings"
	"testing"
)

const ordersPath3Hint = "       how to cover it:\n" +
	"         - make `in.AmountCents <= 0` false\n" +
	"         - let activity ChargeCard succeed (mock it with env.OnActivity, passing the same activity the workflow starts (ChargeCard), with .Return(...); or let it run)\n" +
	"         - expect the workflow to complete\n"

// --hints adds "how to cover it" under each testable path no test ran,
// and nothing else changes; without it the output is exactly as before.
func TestHints(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	plain, _, _ := run(t, "report", "./orders", "--traces", traces)
	out, stderr, code := run(t, "report", "./orders", "--traces", traces, "--hints")
	line3 := "  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed): missed\n"
	if code != ExitOK || stderr != "" || out != strings.Replace(plain, line3, line3+ordersPath3Hint, 1) || !strings.Contains(plain, line3) {
		t.Fatalf("report --hints: code=%d stderr=%q\n%s", code, stderr, out)
	}

	plain, _, _ = run(t, "coverage", "./orders/orders.go", "--traces", traces)
	out, _, _ = run(t, "coverage", "./orders/orders.go", "--traces", traces, "--hints")
	line3 = "  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)\n"
	if out != strings.Replace(plain, line3, line3+ordersPath3Hint, 1) || !strings.Contains(plain, line3) {
		t.Fatalf("coverage --hints:\n%s", out)
	}

	for _, cmd := range [][]string{{"report", "./orders"}, {"coverage", "./orders/orders.go"}} {
		js, _, _ := run(t, append(cmd, "--traces", traces, "--json")...)
		if strings.Contains(js, "howToCover") {
			t.Errorf("%s --json without --hints has howToCover", cmd[0])
		}
		js, _, _ = run(t, append(cmd, "--traces", traces, "--json", "--hints")...)
		paths := decode(t, js)["workflows"].([]any)[0].(map[string]any)["pathList"].([]any)
		for i, p := range paths {
			h, has := p.(map[string]any)["howToCover"]
			if (i == 2) != has {
				t.Errorf("%s path %d: howToCover present=%v", cmd[0], i+1, has)
			}
			if has && len(h.([]any)) != 3 {
				t.Errorf("%s path 3 hints: %v", cmd[0], h)
			}
		}
	}

	_, stderr, _ = run(t, "report", "./orders", "--traces", traces, "--hints", "--summary")
	if !strings.Contains(stderr, "--hints ignored because --summary was passed") {
		t.Errorf("--summary --hints: stderr=%q", stderr)
	}

	// A path that is not testable gets no hint.
	cfg := ciConfig(t, `{"enabled": true, "rules": [{"id": "never-charged", "workflow": "OrderWorkflow", "label": "contradictory",
	  "when": ["ChargeCard (activity) --success"], "impossible": true, "reason": "r"}]}`)
	out, _, _ = run(t, "report", "--traces", traces, "--config", cfg, "--hints")
	if strings.Contains(out, "how to cover it") {
		t.Errorf("a cannot-be-tested path got a hint:\n%s", out)
	}
}
