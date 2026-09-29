package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In the pilot folder (no config there), orders has paths 1 and 2 of 3
// recorded (ordersTraces): 66.7%, Medium.
const ordersReport = `orders.OrderWorkflow (orders/orders.go)
2/3 paths · 66.7% · branches 3/4 · priority Medium
  1. Start -> if in.AmountCents <= 0 --true--> End (completed): covered
  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed): covered
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed): missed

3 paths total · 2 covered · 1 missed · 66.7% project coverage
Branches: 3/4 (75.0%)

0 workflows excluded (no scope in use)
Traces: 2 read · 2 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
`

const ordersReportSummary = `orders.OrderWorkflow  2/3 paths · 66.7% · priority Medium

3 paths total · 2 covered · 1 missed · 66.7% project coverage
Branches: 3/4 (75.0%)

0 workflows excluded (no scope in use)
Traces: 2 read · 2 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
`

// native swaps the one file name in a report text for this system's
// form: the text output uses the system's separator (orders\orders.go on
// Windows), while --json always uses forward slashes.
func native(report string) string {
	return strings.Replace(report, "(orders/orders.go)", "("+filepath.Join("orders", "orders.go")+")", 1)
}

// inPilot runs from the pilot folder, so file names print as orders/orders.go.
func inPilot(t *testing.T) {
	t.Helper()
	t.Chdir(absPath(t, pilot))
}

func TestReportText(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out := filepath.Join(t.TempDir(), "report.txt")
	stdout, stderr, code := run(t, "report", "./orders", "--traces", traces, "--out", out)
	if want := native(ordersReport); code != ExitOK || stderr != "" || stdout != want {
		t.Fatalf("code=%d stderr=%q\n%s\nwant\n%s", code, stderr, stdout, want)
	}
	if data, _ := os.ReadFile(out); string(data) != stdout {
		t.Errorf("--out file differs from stdout:\n%s", data)
	}
	stdout, _, code = run(t, "report", "./orders", "--traces", traces, "--summary")
	if code != ExitOK || stdout != ordersReportSummary {
		t.Errorf("--summary: code=%d\n%s\nwant\n%s", code, stdout, ordersReportSummary)
	}
}

// --summary: exactly one line per workflow, then the footer.
func TestReportSummaryOneLinePerWorkflow(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	stdout, _, code := run(t, "report", "./...", "--traces", traces, "--summary")
	lines := strings.Split(stdout, "\n")
	want := []string{
		"approval.ApprovalWorkflow             0/4 paths · 0.0% · priority High",
		"billing.SubscriptionWorkflow          0/3 paths · 0.0% · priority High",
		"fulfillment.OrderFulfillmentWorkflow  0/4 paths · 0.0% · priority High",
		"fulfillment.PaymentWorkflow           0/4 paths · 0.0% · priority High",
		"orders.OrderWorkflow                  2/3 paths · 66.7% · priority Medium",
		"polling.ReportPollingWorkflow         0/5 paths · 0.0% · priority High",
		"reports.DailyReportWorkflow           0/6 paths · 0.0% · priority High",
		"shipment.ShipmentWorkflow             0/9 paths · 0.0% · priority High",
		"",
		"38 paths total · 2 covered · 36 missed · 5.3% project coverage",
	}
	if code != ExitOK || len(lines) < len(want) || strings.Join(lines[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Errorf("code=%d\n%s", code, stdout)
	}
}

func TestReportJSONGolden(t *testing.T) {
	traces := ordersTraces(t)
	golden := absPath(t, "testdata/report_orders.json")
	inPilot(t)
	stdout, _, code := run(t, "report", "./orders", "--traces", traces, "--json", "--fail-under", "60")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitOK || stdout != string(want) {
		t.Errorf("code=%d, --json differs from testdata/report_orders.json:\n%s", code, stdout)
	}
}

// report --json is coverage --json plus file, priority and excludedBy:
// the same numbers, field by field.
func TestReportJSONIsCoverageJSONPlusThree(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	rep, _, _ := run(t, "report", "./orders", "--traces", traces, "--json")
	cov, _, _ := run(t, "coverage", "./orders", "--traces", traces, "--json")
	if got, want := StripReportOnly(t, rep), decode(t, cov); !jsonEqual(got, want) {
		t.Errorf("report --json minus its three fields differs from coverage --json:\n%s\nvs\n%s", rep, cov)
	}
}

// StripReportOnly decodes report --json and removes the fields coverage
// --json doesn't have.
func StripReportOnly(t *testing.T, s string) map[string]any {
	t.Helper()
	m := decode(t, s)
	delete(m, "excludedBy")
	for _, w := range m["workflows"].([]any) {
		delete(w.(map[string]any), "file")
		delete(w.(map[string]any), "priority")
	}
	return m
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return m
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// The excluded line is always printed, and names what excluded them.
func TestReportExcludedLine(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	tests := []struct {
		name  string
		args  []string
		lines string
	}{
		{"flag", []string{"--exclude", "PaymentWorkflow"},
			"1 workflow excluded by --exclude (see \"reason\"):\n  fulfillment.PaymentWorkflow: excluded by --exclude flag\n"},
		{"include flag", []string{"--include", "PaymentWorkflow"},
			"1 workflow excluded by --include (see \"reason\"):\n  fulfillment.OrderFulfillmentWorkflow: not in include list\n"},
		{"config", []string{"--config", writeConfig(t, `{"packages": [`+jsonString(absPath(t, ".")+"/fulfillment")+`], "workflows": {"exclude": [{"name": "PaymentWorkflow", "reason": "tested elsewhere"}]}}`)},
			"1 workflow excluded by .pathkitrc.json (see \"reason\"):\n  fulfillment.PaymentWorkflow: tested elsewhere\n"},
		{"config and flag", []string{"./...", "--config", writeConfig(t, `{"packages": [`+jsonString(absPath(t, ".")+"/...")+`], "workflows": {"exclude": [{"name": "PaymentWorkflow", "reason": "tested elsewhere"}]}}`), "--exclude", "ShipmentWorkflow"},
			"2 workflows excluded by .pathkitrc.json and --exclude (see \"reason\"):\n  fulfillment.PaymentWorkflow: tested elsewhere\n  shipment.ShipmentWorkflow: excluded by --exclude flag\n"},
		{"config that excludes nothing", []string{"--config", writeConfig(t, `{"packages": [`+jsonString(absPath(t, ".")+"/fulfillment")+`], "workflows": {"include": ["PaymentWorkflow", "OrderFulfillmentWorkflow"]}}`)},
			"0 workflows excluded by .pathkitrc.json\n"},
		{"--all", []string{"--all", "--exclude", "PaymentWorkflow"},
			"0 workflows excluded (no scope in use)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, args := "./fulfillment", tt.args
			if args[0] == "./..." {
				target, args = args[0], args[1:]
			}
			stdout, stderr, code := run(t, append([]string{"report", target, "--traces", traces}, args...)...)
			if code != ExitOK || !strings.Contains(stdout, "\n\n"+tt.lines) || !strings.Contains(stdout, "\nTraces: ") {
				t.Errorf("code=%d stderr=%q\n%s\nwant it to contain\n%s", code, stderr, stdout, tt.lines)
			}
		})
	}
}

func TestReportAddedByConfig(t *testing.T) {
	cfg := absPath(t, "../../testdata/scopes/added/.pathkitrc.json")
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "scope.lowerFlow",
		"functionHash": "0000000000000000", "status": "complete", "steps": []string{}})
	stdout, _, code := run(t, "report", "--config", cfg, "--traces", traces, "--summary")
	if code != ExitOK || !strings.Contains(stdout, "scope.lowerFlow (added by config)  ") {
		t.Errorf("code=%d\n%s", code, stdout)
	}
}

func TestReportFailUnder(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	tests := []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"--fail-under", "60"}, ExitOK, ""},
		{[]string{"--fail-under", "66.7"}, ExitBelowThreshold, "pathkit report: coverage 66.67% is below --fail-under 66.7%\n"},
		{[]string{"--fail-under", "80", "--json"}, ExitBelowThreshold, "pathkit report: coverage 66.7% is below --fail-under 80%\n"},
		{[]string{"--config", writeConfig(t, `{"failUnder": 90}`)}, ExitBelowThreshold, "is below failUnder in "},
		{[]string{"--config", writeConfig(t, `{"failUnder": 90}`), "--fail-under", "50"}, ExitOK, ""}, // the flag wins
	}
	for _, tt := range tests {
		stdout, stderr, code := run(t, append([]string{"report", "./orders", "--traces", traces}, tt.args...)...)
		if code != tt.code || !strings.Contains(stderr, tt.stderr) || (tt.stderr == "" && stderr != "") {
			t.Errorf("%v: code=%d stderr=%q, want code %d and %q", tt.args, code, stderr, tt.code, tt.stderr)
		}
		if !strings.Contains(stdout, "66.7") {
			t.Errorf("%v: the report must be printed before exit 2:\n%s", tt.args, stdout)
		}
	}
}

// Config keys out, json and noColor apply to report; a flag beats them.
func TestReportConfigKeys(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	cfg := writeConfig(t, `{"out": "report.txt", "json": true, "noColor": true}`)
	outFile := filepath.Join(filepath.Dir(cfg), "report.txt") // relative to the config file

	stdout, _, code := run(t, "report", "./orders", "--traces", traces, "--config", cfg)
	if code != ExitOK || !strings.HasPrefix(stdout, "{") {
		t.Fatalf("config json: code=%d\n%s", code, stdout)
	}
	if data, err := os.ReadFile(outFile); err != nil || string(data) != stdout {
		t.Errorf("config out: %v\n%s", err, data)
	}

	stdout, _, _ = run(t, "report", "./orders", "--traces", traces, "--config", cfg, "--json=false", "--out", filepath.Join(t.TempDir(), "x.txt"))
	if stdout != native(ordersReport) {
		t.Errorf("--json=false must beat the config:\n%s", stdout)
	}
}

func TestReportClean(t *testing.T) {
	left := func(dir string) int {
		files, _ := filepath.Glob(filepath.Join(dir, "*.trace.json"))
		return len(files)
	}
	traces := ordersTraces(t)
	inPilot(t)
	_, stderr, code := run(t, "report", "./orders", "--traces", traces, "--clean")
	if code != ExitOK || left(traces) != 0 || !strings.Contains(stderr, "pathkit report: deleted 2 trace files from "+traces) {
		t.Errorf("exit 0: code=%d, %d left, stderr=%q", code, left(traces), stderr)
	}
	traces = ordersTraces(t)
	if _, _, code := run(t, "report", "./orders", "--traces", traces, "--clean", "--fail-under", "90"); code != ExitBelowThreshold || left(traces) != 0 {
		t.Errorf("exit 2: code=%d, %d left; want them deleted", code, left(traces))
	}
	traces = ordersTraces(t)
	badOut := filepath.Join(t.TempDir(), "no", "such", "folder", "out.txt")
	if _, _, code := run(t, "report", "./orders", "--traces", traces, "--clean", "--out", badOut); code != ExitError || left(traces) != 2 {
		t.Errorf("exit 1: code=%d, %d left; want all kept", code, left(traces))
	}
}

func TestReportErrors(t *testing.T) {
	traces := ordersTraces(t)
	empty := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string // stderr prefix
	}{
		{"a file", []string{"report", pilot + "/orders/orders.go"},
			"pathkit report: expected a folder or folder/..., not a file: " + pilot + "/orders/orders.go (report covers a whole project; use \"pathkit coverage\" for one file)\n"},
		{"two folders", []string{"report", pilot, pilot},
			"pathkit report: expected at most one <folder> argument, got 2\n"},
		{"not analyzable", []string{"report", fixtures + "/rules/...", "--traces", traces},
			"pathkit report: in scope but not analyzable: rules.SelectorAddAfterSelect: "},
		{"broken package", []string{"report", fixtures + "/...", "--traces", traces},
			"pathkit report: package does not compile: example.com/fixtures/broken: "},
		{"no traces", []string{"report", pilot + "/orders", "--traces", empty},
			"pathkit report: no trace files found in " + empty + `; coverage is recorded only by "pathkit test" (plain "go test" records nothing)` + "\n"},
		{"missing folder", []string{"report", "nowhere/..."},
			"pathkit report: Directory not found: nowhere\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := run(t, tt.args...)
			if code != ExitError || !strings.HasPrefix(stderr, tt.want) {
				t.Errorf("code=%d stderr=%q, want prefix %q", code, stderr, tt.want)
			}
		})
	}
	// Not analyzable ends with the fix-it hint.
	_, stderr, _ := run(t, "report", fixtures+"/rules/...", "--traces", traces)
	if !strings.HasSuffix(stderr, notAnalyzableHint+"\n") {
		t.Errorf("stderr = %q", stderr)
	}
}

// No folder: every entry of the config's "packages" (unlike test/traces,
// which need exactly one).
func TestReportEveryConfiguredPackage(t *testing.T) {
	traces := ordersTraces(t)
	p := absPath(t, pilot)
	t.Chdir(t.TempDir())
	cfg := writeConfig(t, `{"packages": [`+jsonString(p+"/orders")+`, `+jsonString(p+"/billing")+`, `+jsonString(p+"/...")+`]}`)
	stdout, _, code := run(t, "report", "--config", cfg, "--traces", traces, "--summary")
	if code != ExitOK || !strings.Contains(stdout, "38 paths total · 2 covered") || strings.Count(stdout, "orders.OrderWorkflow") != 1 {
		t.Errorf("code=%d (overlapping entries must count each workflow once)\n%s", code, stdout)
	}
}
