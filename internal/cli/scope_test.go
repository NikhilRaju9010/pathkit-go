package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func absPath(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// writeConfig writes a .pathkitrc.json into a new temp folder and
// returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var (
	totalRe    = regexp.MustCompile(`(?m)^Total paths: (\d+)$`)
	workflowRe = regexp.MustCompile(`(?m)^Workflow: `)
)

// workflows counts the "Workflow: ..." header lines (not the excluded
// list, whose names also end in "Workflow: ").
func workflows(stdout string) int { return len(workflowRe.FindAllString(stdout, -1)) }

// totalPaths adds up every "Total paths: N" line.
func totalPaths(t *testing.T, stdout string) int {
	t.Helper()
	sum := 0
	for _, m := range totalRe.FindAllStringSubmatch(stdout, -1) {
		n, _ := strconv.Atoi(m[1])
		sum += n
	}
	return sum
}

// No config file: everything is in scope, exactly as before M5.
func TestNoConfigEverythingInScope(t *testing.T) {
	p := absPath(t, pilot)
	t.Chdir(t.TempDir())
	stdout, stderr, code := run(t, "analyze", p+"/...", "--summary")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if n := workflows(stdout); n != 8 || totalPaths(t, stdout) != 38 {
		t.Errorf("printed %d workflows, %d paths; want 8 and 38", n, totalPaths(t, stdout))
	}
	if strings.Contains(stdout, "Excluded") || strings.Contains(stdout, "added by config") {
		t.Errorf("no config, yet scope output appeared:\n%s", stdout)
	}
}

const shipmentExcluded = "\nExcluded from scope (1), pass --all to show them:\n  shipment.ShipmentWorkflow: needs a real carrier sandbox\n"

func TestAnalyzeWithScopeConfig(t *testing.T) {
	cfg := absPath(t, "../../testdata/scopes/no-shipment/.pathkitrc.json")
	p := absPath(t, pilot)
	t.Chdir(t.TempDir())
	stdout, stderr, code := run(t, "analyze", p+"/...", "--summary", "--config", cfg)
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if n := workflows(stdout); n != 7 || totalPaths(t, stdout) != 29 {
		t.Errorf("printed %d workflows, %d paths; want 7 and 29", n, totalPaths(t, stdout))
	}
	if strings.Contains(stdout, "Workflow: shipment.") || !strings.HasSuffix(stdout, shipmentExcluded) {
		t.Errorf("shipment must be listed as excluded, with its reason, and not analyzed:\n%s", stdout)
	}

	// --all ignores the config, and says so.
	stdout, stderr, code = run(t, "analyze", p+"/...", "--summary", "--config", cfg, "--all")
	if code != ExitOK || workflows(stdout) != 8 || strings.Contains(stdout, "Excluded") {
		t.Errorf("--all: code=%d, want all 8 and no excluded list:\n%s", code, stdout)
	}
	if want := "pathkit analyze: --all: " + cfg + " is ignored; showing every workflow\n"; stderr != want {
		t.Errorf("--all stderr = %q, want %q", stderr, want)
	}
}

// The config is found by searching up from the current folder.
func TestAnalyzeFindsConfigUpward(t *testing.T) {
	p := absPath(t, pilot)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"packages": ["` + p + `/..."], "workflows": {"exclude": [{"name": "OrderWorkflow", "reason": "retired"}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".pathkitrc.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	stdout, _, code := run(t, "analyze", p+"/orders/orders.go")
	if code != ExitError || !strings.Contains(stdout, "  orders.OrderWorkflow: retired\n") {
		t.Errorf("code=%d, want the excluded list (and exit 1: nothing left in scope):\n%s", code, stdout)
	}
}

func TestAnalyzeScopeFlags(t *testing.T) {
	p := absPath(t, pilot)
	t.Chdir(t.TempDir())

	stdout, _, code := run(t, "analyze", p+"/...", "--summary", "--exclude", "ShipmentWorkflow,orders.OrderWorkflow")
	if code != ExitOK || !strings.HasSuffix(stdout, "\nExcluded from scope (2), pass --all to show them:\n"+
		"  orders.OrderWorkflow: excluded by --exclude flag\n  shipment.ShipmentWorkflow: excluded by --exclude flag\n") {
		t.Errorf("--exclude: code=%d\n%s", code, stdout)
	}

	stdout, _, code = run(t, "analyze", p+"/...", "--summary", "--include", "OrderWorkflow")
	if code != ExitOK || workflows(stdout) != 1 || strings.Count(stdout, ": not in include list\n") != 7 {
		t.Errorf("--include: code=%d, want 1 workflow and 7 'not in include list':\n%s", code, stdout)
	}

	_, stderr, code := run(t, "analyze", p+"/...", "--exclude", "ShipmentWorkflw")
	if want := `pathkit analyze: --exclude "ShipmentWorkflw" matches no workflow in ` + p + "/...\n"; code != ExitError || stderr != want {
		t.Errorf("misspelled --exclude: code=%d stderr=%q, want %q", code, stderr, want)
	}
}

func TestAnalyzeAddedByConfig(t *testing.T) {
	cfg := absPath(t, "../../testdata/scopes/added/.pathkitrc.json")
	f := absPath(t, fixtures)
	t.Chdir(t.TempDir())
	stdout, _, code := run(t, "analyze", f+"/scope/...", "--summary", "--config", cfg)
	if code != ExitOK || !strings.Contains(stdout, "Workflow: scope.lowerFlow (added by config)\n") ||
		!strings.Contains(stdout, "Workflow: scope.VisibleFlow\n") || !strings.Contains(stdout, "  a.Run: not in include list\n") {
		t.Errorf("code=%d:\n%s", code, stdout)
	}
}

func TestAnalyzeConfigErrors(t *testing.T) {
	p := absPath(t, pilot)
	f := absPath(t, fixtures)
	t.Chdir(t.TempDir())
	tests := []struct{ name, config, want string }{
		{"invalid JSON", "{\n  \"workflows\": {\n    \"exclude\": [ { \"name\": \"X\" \"reason\": \"y\" } ]\n  }\n}",
			"invalid JSON at line 3, column 32: invalid character '\"' after object key:value pair\n"},
		{"empty include", `{"workflows": {"include": []}}`,
			"workflows.include is empty: remove it to include every workflow, or list the ones you want\n"},
		{"misspelled include", `{"packages": ["` + p + `/..."], "workflows": {"include": ["OrderWorkflw"]}}`,
			`include "OrderWorkflw" matches no function in the configured packages (` + p + "/...)\n"},
		{"include of a non-workflow", `{"packages": ["` + f + `/scope/..."], "workflows": {"include": ["sendEmail"]}}`,
			`include "sendEmail" is not a workflow: its first parameter is not workflow.Context` + "\n"},
		{"unknown key", `{"workflow": {}}`, `unknown key "workflow"` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := writeConfig(t, tt.config)
			_, stderr, code := run(t, "analyze", p+"/orders/orders.go", "--config", cfg)
			if want := "pathkit analyze: " + cfg + ": " + tt.want; code != ExitError || stderr != want {
				t.Errorf("code=%d\nstderr %q\nwant   %q", code, stderr, want)
			}
		})
	}
}

// An in-scope workflow PathKit can't map is always printed; excluding it
// with a reason moves it to the excluded list instead.
func TestAnalyzeNotAnalyzable(t *testing.T) {
	file := absPath(t, fixtures+"/rules/unsupported.go")
	rules := absPath(t, fixtures+"/rules")
	t.Chdir(t.TempDir())

	_, stderr, code := run(t, "analyze", file)
	if code != ExitError || !strings.Contains(stderr, "pathkit analyze: in scope but not analyzable: rules.UsesLabel: goto at unsupported.go:21 is not supported: "+
		"PathKit maps break, continue and return, but not goto (fix it, or exclude it in .pathkitrc.json with a reason)\n") {
		t.Errorf("code=%d stderr:\n%s", code, stderr)
	}

	cfg := writeConfig(t, `{"packages": ["`+rules+`"], "workflows": {"exclude": [
		{"name": "UsesLabel", "reason": "uses goto on purpose"},
		{"name": "UsesGoSelect", "reason": "uses Go's select on purpose"}]}}`)
	stdout, stderr, code := run(t, "analyze", file, "--config", cfg)
	if strings.Contains(stderr, "not analyzable") || !strings.Contains(stdout, "  rules.UsesLabel: uses goto on purpose\n") ||
		stderr != "pathkit analyze: no workflows in scope to analyze (every workflow is excluded)\n" || code != ExitError {
		t.Errorf("code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// failUnder, html, out, json, noColor and allowStale are checked but not
// used until M6–M8: a config that sets them all changes nothing, and no
// command mentions them.
func TestLaterKeysAreNotApplied(t *testing.T) {
	p := absPath(t, pilot)
	dir := t.TempDir()
	t.Chdir(dir)
	plain, _, _ := run(t, "analyze", p+"/orders/orders.go")
	cfg := writeConfig(t, `{"failUnder": 90, "html": "report.html", "out": "out.txt", "json": true, "noColor": true, "allowStale": true}`)
	stdout, stderr, code := run(t, "analyze", p+"/orders/orders.go", "--config", cfg)
	if code != ExitOK || stdout != plain || stderr != "" {
		t.Errorf("a config with only later keys changed the output: code=%d stderr=%q\n%s", code, stderr, stdout)
	}
	for _, f := range []string{"out.txt", "report.html", filepath.Join(filepath.Dir(cfg), "out.txt"), filepath.Join(filepath.Dir(cfg), "report.html")} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s was written; out/html must not be applied before M7/M8", f)
		}
	}
}

// A trace from an excluded workflow is listed as excluded, not unmatched.
func TestTracesExcluded(t *testing.T) {
	p := absPath(t, pilot)
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	writeTrace(t, dir, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
		"functionHash": "0000000000000000", "status": "complete", "steps": []string{"J1.true"}})
	stdout, _, code := run(t, "traces", p+"/orders", "--traces", dir, "--exclude", "OrderWorkflow")
	want := "orders.OrderWorkflow: excluded from scope (excluded by --exclude flag) [x.trace.json]\n\n1 trace: 1 excluded\n"
	if code != ExitOK || stdout != want {
		t.Errorf("code=%d\n%q\nwant\n%q", code, stdout, want)
	}
}
