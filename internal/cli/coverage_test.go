package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// ordersTraces writes traces for orders paths 1 and 2 (2 of 3 covered,
// 66.7%) into a new folder, with the current code's hash.
func ordersTraces(t *testing.T) string {
	t.Helper()
	res, err := load.Load(pilot + "/orders")
	if err != nil {
		t.Fatal(err)
	}
	wf := discover.Find(res.Packages)[0]
	hash := model.FunctionHash(wf.Pkg.Fset, wf.Func)
	dir := t.TempDir()
	for name, steps := range map[string][]string{"a": {"J1.true"}, "b": {"J1.false", "J2.failure"}} {
		writeTrace(t, dir, name, map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
			"functionHash": hash, "status": "complete", "steps": steps})
	}
	return dir
}

const ordersCoverage = `Workflow: orders.OrderWorkflow
Total paths: 3
Covered: 2/3 (66.7%)
Branches: 3/4 (75.0%)

Covered paths:
  1. Start -> if in.AmountCents <= 0 --true--> End (completed)
  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)
Untested paths:
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)

Traces: 2 read · 2 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
`

func TestCoverageText(t *testing.T) {
	traces := ordersTraces(t)
	out := filepath.Join(t.TempDir(), "report.txt")
	stdout, stderr, code := run(t, "coverage", pilot+"/orders/orders.go", "--traces", traces, "--out", out)
	if code != ExitOK || stderr != "" || stdout != ordersCoverage {
		t.Fatalf("code=%d stderr=%q\n%s\nwant\n%s", code, stderr, stdout, ordersCoverage)
	}
	if data, _ := os.ReadFile(out); string(data) != stdout {
		t.Errorf("--out file differs from stdout:\n%s", data)
	}
}

func TestCoverageJSONGolden(t *testing.T) {
	stdout, _, code := run(t, "coverage", pilot+"/orders/orders.go", "--traces", ordersTraces(t), "--json", "--fail-under", "60")
	want, err := os.ReadFile("testdata/coverage_orders.json")
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitOK || stdout != string(want) {
		t.Errorf("code=%d, --json differs from testdata/coverage_orders.json:\n%s", code, stdout)
	}
}

func TestCoverageFailUnder(t *testing.T) {
	traces := ordersTraces(t)
	file := pilot + "/orders/orders.go"
	tests := []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"--fail-under", "50"}, ExitOK, ""},
		{[]string{"--fail-under", "70"}, ExitBelowThreshold, "pathkit coverage: coverage 66.7% is below --fail-under 70%\n"},
		// 66.67 would print as 66.7 at one decimal: the difference is shown
		{[]string{"--fail-under", "66.7"}, ExitBelowThreshold, "pathkit coverage: coverage 66.67% is below --fail-under 66.7%\n"},
		{[]string{"--fail-under", "66.6"}, ExitOK, ""},
		{[]string{"--fail-under", "101"}, ExitError, "pathkit coverage: invalid --fail-under value: \"101\" (it must be a number from 0 to 100)\n"},
	}
	for _, tt := range tests {
		stdout, stderr, code := run(t, append([]string{"coverage", file, "--traces", traces}, tt.args...)...)
		if code != tt.code || stderr != tt.stderr {
			t.Errorf("%v: code=%d stderr=%q; want %d %q", tt.args, code, stderr, tt.code, tt.stderr)
		}
		if tt.code == ExitBelowThreshold && !strings.Contains(stdout, "Covered: 2/3") {
			t.Errorf("%v: the report must be printed before exit 2:\n%s", tt.args, stdout)
		}
	}
}

// The config's failUnder applies; --fail-under overrides it.
func TestCoverageFailUnderFromConfig(t *testing.T) {
	traces := ordersTraces(t)
	p := absPath(t, pilot)
	cfg := writeConfig(t, `{"failUnder": 90}`)
	_, stderr, code := run(t, "coverage", p+"/orders/orders.go", "--traces", traces, "--config", cfg)
	if code != ExitBelowThreshold || stderr != "pathkit coverage: coverage 66.7% is below failUnder in "+cfg+" 90%\n" {
		t.Errorf("config failUnder: code=%d stderr=%q", code, stderr)
	}
	if _, _, code := run(t, "coverage", p+"/orders/orders.go", "--traces", traces, "--config", cfg, "--fail-under", "10"); code != ExitOK {
		t.Errorf("--fail-under 10 must override the config: code=%d", code)
	}
}

// Owner's condition (M6): --clean deletes traces only when the report was
// fully produced (exit 0 or 2), never after an error (exit 1).
func TestCoverageClean(t *testing.T) {
	file := pilot + "/orders/orders.go"
	left := func(dir string) int {
		files, _ := filepath.Glob(filepath.Join(dir, "*.trace.json"))
		return len(files)
	}

	traces := ordersTraces(t)
	os.WriteFile(filepath.Join(traces, "notes.txt"), []byte("keep me"), 0o644)
	_, stderr, code := run(t, "coverage", file, "--traces", traces, "--clean")
	if code != ExitOK || left(traces) != 0 || !strings.Contains(stderr, "pathkit coverage: deleted 2 trace files from "+traces) {
		t.Errorf("exit 0: code=%d, %d traces left, stderr=%q", code, left(traces), stderr)
	}
	if _, err := os.Stat(filepath.Join(traces, "notes.txt")); err != nil {
		t.Errorf("--clean deleted a file that is not a trace")
	}

	traces = ordersTraces(t)
	_, _, code = run(t, "coverage", file, "--traces", traces, "--clean", "--fail-under", "90")
	if code != ExitBelowThreshold || left(traces) != 0 {
		t.Errorf("exit 2: code=%d, %d traces left; want them deleted", code, left(traces))
	}

	traces = ordersTraces(t)
	badOut := filepath.Join(t.TempDir(), "no", "such", "folder", "out.txt")
	stdout, _, code := run(t, "coverage", file, "--traces", traces, "--clean", "--out", badOut)
	if code != ExitError || left(traces) != 2 || !strings.Contains(stdout, "Covered: 2/3") {
		t.Errorf("exit 1 (--out failed after printing): code=%d, %d traces left; want all 2 kept", code, left(traces))
	}

	traces = ordersTraces(t)
	if _, _, code := run(t, "coverage", fixtures+"/rules/unsupported.go", "--traces", traces, "--clean"); code != ExitError || left(traces) != 2 {
		t.Errorf("exit 1 (not analyzable): code=%d, %d traces left; want all 2 kept", code, left(traces))
	}
}

func TestCoverageErrors(t *testing.T) {
	empty := t.TempDir()
	_, stderr, code := run(t, "coverage", pilot+"/orders/orders.go", "--traces", empty)
	if want := "pathkit coverage: no trace files found in " + empty + `; coverage is recorded only by "pathkit test" (plain "go test" records nothing)` + "\n"; code != ExitError || stderr != want {
		t.Errorf("no traces: code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = run(t, "coverage", fixtures+"/rules/unsupported.go", "--traces", empty)
	if code != ExitError || !strings.HasPrefix(stderr, "pathkit coverage: in scope but not analyzable: rules.UsesGoSelect: ") ||
		!strings.Contains(stderr, "; rules.UsesLabel: goto at unsupported.go:21 is not supported") ||
		!strings.HasSuffix(stderr, "(fix it, or exclude it in .pathkitrc.json with a reason)\n") {
		t.Errorf("not analyzable: code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = run(t, "coverage", pilot+"/...", "--traces", ordersTraces(t), "--function", "OrderWorkflw")
	if code != ExitError || !strings.HasSuffix(stderr, `matches no workflow in `+pilot+`/...; did you mean "OrderWorkflow"?`+"\n") {
		t.Errorf("--function typo: code=%d stderr=%q", code, stderr)
	}
}

func TestCoverageFunction(t *testing.T) {
	stdout, _, code := run(t, "coverage", pilot+"/...", "--traces", ordersTraces(t), "--function", "OrderWorkflow")
	if code != ExitOK || workflows(stdout) != 1 || !strings.Contains(stdout, "Covered: 2/3 (66.7%)") {
		t.Errorf("code=%d\n%s", code, stdout)
	}
}

// Warnings for traces that don't count go to stderr and never change the
// exit code (owner's decision, M6).
func TestCoverageWarnings(t *testing.T) {
	traces := ordersTraces(t)
	writeTrace(t, traces, "c-unmatched", map[string]any{"schemaVersion": 1, "workflow": "orders.OrderWorkflow",
		"functionHash": "x", "status": "complete", "steps": []string{"J1.true"}})
	stdout, stderr, code := run(t, "coverage", pilot+"/orders/orders.go", "--traces", traces)
	if code != ExitOK || stderr != "pathkit coverage: warning: trace c-unmatched.trace.json was recorded for an older version of orders.OrderWorkflow; re-run pathkit test, or pass --allow-stale\n" ||
		!strings.Contains(stdout, "Traces: 3 read · 2 counted · 0 unmatched · 1 stale") {
		t.Errorf("code=%d stderr=%q\n%s", code, stderr, stdout)
	}
}

func TestClean(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"old", "new"} {
		writeTrace(t, dir, n, map[string]any{"schemaVersion": 1})
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o644)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(dir, "old.trace.json"), old, old)

	stdout, _, code := run(t, "clean", "--traces", dir, "--older-than", "1d")
	if code != ExitOK || stdout != "pathkit clean: deleted 1 trace file from "+dir+"\n" {
		t.Errorf("--older-than: code=%d %q", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.trace.json")); err != nil {
		t.Errorf("the newer trace was deleted")
	}
	stdout, _, _ = run(t, "clean", "--traces", dir)
	if stdout != "pathkit clean: deleted 1 trace file from "+dir+"\n" {
		t.Errorf("clean all: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("clean deleted a file that is not a trace")
	}
	missing := filepath.Join(dir, "missing")
	if stdout, _, code := run(t, "clean", "--traces", missing); code != ExitOK || stdout != "pathkit clean: nothing to clean in "+missing+"\n" {
		t.Errorf("missing folder: code=%d %q", code, stdout)
	}
	if _, stderr, code := run(t, "clean", "--traces", dir, "--older-than", "soon"); code != ExitError ||
		stderr != `pathkit clean: invalid --older-than value: "soon" (use a number with m, h or d, e.g. 7d)`+"\n" {
		t.Errorf("bad --older-than: code=%d %q", code, stderr)
	}
}
