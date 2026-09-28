package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pilot    = "../../testdata/pilot"
	fixtures = "../../testdata/fixtures"
)

func TestAnalyzeOrdersText(t *testing.T) {
	stdout, stderr, code := run(t, "analyze", pilot+"/orders/orders.go")
	want := `Workflow: orders.OrderWorkflow
Total paths: 3

  1. Start -> if in.AmountCents <= 0 --true--> End (completed)

  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)

  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
`
	if code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("code=%d stderr=%q\nstdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
}

func TestAnalyzeSummaryAfterFile(t *testing.T) {
	// The flag comes after the file name and must still be read.
	stdout, _, code := run(t, "analyze", pilot+"/reports/dailyreport.go", "--summary")
	if want := "Workflow: reports.DailyReportWorkflow\nTotal paths: 6\n"; code != ExitOK || stdout != want {
		t.Errorf("code=%d stdout=%q, want %q", code, stdout, want)
	}
}

func TestAnalyzeLimit(t *testing.T) {
	stdout, _, code := run(t, "analyze", "--limit", "2", pilot+"/reports/dailyreport.go")
	if code != ExitOK || strings.Count(stdout, "Start ->") != 2 ||
		!strings.HasSuffix(stdout, "\n  ... and 4 more paths (use --summary or increase --limit to see them)\n") {
		t.Errorf("code=%d stdout:\n%s", code, stdout)
	}
}

func TestAnalyzeSummaryWithLimitNotes(t *testing.T) {
	stdout, stderr, code := run(t, "analyze", pilot+"/reports/dailyreport.go", "--summary", "--limit", "2")
	if code != ExitOK || stderr != "pathkit analyze: --limit ignored because --summary was passed.\n" ||
		stdout != "Workflow: reports.DailyReportWorkflow\nTotal paths: 6\n" {
		t.Errorf("code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

func TestAnalyzeMermaid(t *testing.T) {
	stdout, _, code := run(t, "analyze", pilot+"/orders/orders.go", "--mermaid")
	want := "Workflow: orders.OrderWorkflow\nTotal paths: 3\n\n```mermaid\nflowchart TD\n" +
		"  n0([\"Start\"])\n" +
		"  n1{\"if in.AmountCents &lt;= 0\"}\n" +
		"  n2{\"ChargeCard (activity)\"}\n" +
		"  e0([\"End (completed)\"])\n" +
		"  e1([\"End (failed)\"])\n" +
		"  n0 --> n1\n" +
		"  n1 -->|true| e0\n" +
		"  n1 -->|false| n2\n" +
		"  n2 -->|failure| e1\n" +
		"  n2 -->|success| e0\n" +
		"```\n"
	if code != ExitOK || stdout != want {
		t.Errorf("code=%d\nstdout:\n%s\nwant:\n%s", code, stdout, want)
	}
}

func TestAnalyzeOut(t *testing.T) {
	out := filepath.Join(t.TempDir(), "paths.txt")
	stdout, _, code := run(t, "analyze", pilot+"/fulfillment/payment.go", "--out", out)
	written, err := os.ReadFile(out)
	if code != ExitOK || err != nil || string(written) != stdout || !strings.Contains(stdout, "Total paths: 4") {
		t.Errorf("code=%d err=%v\nstdout:\n%s\nfile:\n%s", code, err, stdout, written)
	}
}

func TestAnalyzeWholePilotSkipsM4Workflows(t *testing.T) {
	stdout, stderr, code := run(t, "analyze", pilot+"/...")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, name := range []string{"orders.OrderWorkflow", "fulfillment.PaymentWorkflow", "reports.DailyReportWorkflow",
		"polling.ReportPollingWorkflow", "billing.SubscriptionWorkflow"} {
		if !strings.Contains(stdout, "Workflow: "+name+"\n") {
			t.Errorf("stdout is missing %s", name)
		}
	}
	if n := strings.Count(stdout, "Workflow: "); n != 5 {
		t.Errorf("printed %d workflows, want 5", n)
	}
	if n := strings.Count(stderr, "pathkit analyze: skipping "); n != 3 || strings.Count(stderr, "is supported from M4\n") != 3 {
		t.Errorf("stderr should have 3 skip lines (approval, shipment, fulfillment saga), got:\n%s", stderr)
	}
	for _, notWorkflow := range []string{"newChildCtx", "AuditLog"} {
		if strings.Contains(stdout+stderr, notWorkflow) {
			t.Errorf("%s must never be treated as a workflow", notWorkflow)
		}
	}
}

func TestAnalyzeTruncatedTotal(t *testing.T) {
	// TwelveIfs has 4,096 paths; the listing stops at 2,000 and says so.
	stdout, _, code := run(t, "analyze", fixtures+"/rules/misc.go", "--summary")
	want := "Workflow: rules.TwelveIfs\nTotal paths: 2000+ (truncated at maxPaths=2000)\n"
	if code != ExitOK || !strings.Contains(stdout, want) {
		t.Errorf("code=%d stdout:\n%s\nwant it to contain:\n%s", code, stdout, want)
	}
}

func TestAnalyzeMermaidDropsPanicRoad(t *testing.T) {
	// The true side of "if x < 0" panics: it is not a path, so the diagram
	// has no arrow for it.
	stdout, _, code := run(t, "analyze", fixtures+"/rules/plain.go", "--mermaid")
	want := "Workflow: rules.Panics\nTotal paths: 1\n\n```mermaid\nflowchart TD\n" +
		"  n0([\"Start\"])\n" +
		"  n1{\"if x &lt; 0\"}\n" +
		"  e0([\"End (completed)\"])\n" +
		"  n0 --> n1\n" +
		"  n1 -->|false| e0\n" +
		"```\n"
	if code != ExitOK || !strings.Contains(stdout, want) {
		t.Errorf("code=%d stdout:\n%s\nwant it to contain:\n%s", code, stdout, want)
	}
}

func TestAnalyzeOutIntoMissingFolder(t *testing.T) {
	out := filepath.Join(t.TempDir(), "no-such-folder", "paths.txt")
	_, stderr, code := run(t, "analyze", pilot+"/orders/orders.go", "--out", out)
	if code != ExitError || !strings.HasPrefix(stderr, "pathkit analyze: could not write --out file: ") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s should not exist", out)
	}
}

func TestAnalyzeErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // stderr prefix
	}{
		{"bad limit", []string{"analyze", pilot + "/orders/orders.go", "--limit", "x"},
			"pathkit analyze: invalid --limit value: \"x\""},
		{"zero limit", []string{"analyze", pilot + "/orders/orders.go", "--limit=0"},
			"pathkit analyze: invalid --limit value: \"0\""},
		{"no workflows in file", []string{"analyze", fixtures + "/rules/noworkflows.go"},
			"pathkit analyze: " + fixtures + "/rules/noworkflows.go has no exported workflow functions to analyze"},
		{"does not compile", []string{"analyze", fixtures + "/broken"},
			"pathkit analyze: package does not compile: "},
		{"every workflow skipped", []string{"analyze", pilot + "/approval/approval.go"},
			"pathkit analyze: skipping approval.ApprovalWorkflow: "},
		{"missing folder", []string{"analyze", "nowhere/..."},
			"pathkit analyze: Directory not found: nowhere"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := run(t, tt.args...)
			if code != ExitError || !strings.HasPrefix(stderr, tt.want) {
				t.Errorf("code=%d stderr=%q, want prefix %q", code, stderr, tt.want)
			}
		})
	}
	// The every-workflow-skipped case ends with this line.
	_, stderr, _ := run(t, "analyze", pilot+"/approval/approval.go")
	if !strings.HasSuffix(stderr, "pathkit analyze: no workflows could be analyzed\n") {
		t.Errorf("stderr = %q", stderr)
	}
}
