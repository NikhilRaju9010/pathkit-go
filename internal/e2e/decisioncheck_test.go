package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The decision check forces each covered decision the other way and re-runs the
// tests that went through it. A test that runs the code and checks nothing lets
// the change through ("survived"); a test that checks the result notices it.
func TestDecisionCheckWeakAndGoodTests(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the check runs the tests with no network, which needs unshare (Linux)")
	}
	if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("no network namespace here: %v %s", err, out)
	}
	root := abs(t, "../../testdata/fixtures/decisions")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", root, "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	out, stderr, code := pathkit(t, "check-decisions", root, "--traces", traces, "--json", "-p", "2")
	if code != 0 {
		t.Fatalf("check-decisions: exit %d\n%s\n%s", code, out, stderr)
	}
	var dc struct {
		Checked, Noticed, Survived int
		NoVisibleEffect            int `json:"noVisibleEffect"`
		Mutants                    []struct {
			Workflow, Decision, Outcome, Result string
		}
		Survivors []struct{ Workflow, Sentence string }
	}
	if err := json.Unmarshal([]byte(out), &dc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	byWf := map[string]map[string]int{}
	for _, m := range dc.Mutants {
		if byWf[m.Workflow] == nil {
			byWf[m.Workflow] = map[string]int{}
		}
		byWf[m.Workflow][m.Result]++
	}
	good, weak := byWf["decisions.Checked"], byWf["decisions.Unchecked"]
	if good["noticed"] != 4 || good["survived"] != 0 {
		t.Errorf("the test that checks the result should notice all 4 forced decisions: %v", good)
	}
	// the two forced ifs only change a local value ("big" or "small") that nothing
	// outside the workflow sees: "no visible effect"; the two forced activity
	// outcomes survive for real
	if weak["survived"] != 2 || weak["no visible effect"] != 2 || weak["noticed"] != 0 {
		t.Errorf("the test that checks nothing should let all 4 through: %v", weak)
	}
	if dc.Noticed != 4 || dc.Survived != 2 || dc.NoVisibleEffect != 2 || dc.Checked != 8 || len(dc.Survivors) != 2 {
		t.Errorf("totals: %+v", dc)
	}
	// the result is kept, and the report shows the score
	if _, err := os.Stat(filepath.Join(".pathkit", "decision-check.json")); err != nil {
		t.Errorf("the result was not kept: %v", err)
	}
	rep, _, _ := pathkitDefault(t, "report", root, "--traces", traces)
	if want := "Tests notice wrong decisions: 66.7% (last check "; !containsStr(rep, want) {
		t.Errorf("the report lacks %q:\n%s", want, rep)
	}
	if !containsStr(rep, "Decisions no test noticed") || !containsStr(rep, "decisions/decisions.go") {
		t.Errorf("the report lists no survivors:\n%s", rep)
	}
	// near miss: no recorded traces were added by the forced runs
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	if len(files) != 6 {
		t.Errorf("%d trace files: the forced runs must leave none", len(files))
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The tests often live in a folder of their own (tests/), not next to the
// workflow. The check must run those tests: running only the workflow's folder
// ran nothing and reported every decision as "no test notices it".
func TestDecisionCheckTestsInAnotherFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the check runs the tests with no network, which needs unshare (Linux)")
	}
	if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("no network namespace here: %v %s", err, out)
	}
	src := abs(t, "../../testdata/fixtures")
	mod := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum"} {
		if data, err := os.ReadFile(filepath.Join(src, f)); err == nil {
			writeFile(t, filepath.Join(mod, f), string(data))
		}
	}
	code, err := os.ReadFile(filepath.Join(src, "decisions", "decisions.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mod, "decisions", "decisions.go"), string(code))
	tests, err := os.ReadFile(filepath.Join(src, "decisions", "decisions_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(tests)
	s = strings.Replace(s, "package decisions", "package tests", 1)
	s = strings.Replace(s, "import (", "import (\n\t. \"example.com/fixtures/decisions\"", 1)
	writeFile(t, filepath.Join(mod, "tests", "decisions_test.go"), s)

	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, c := pathkit(t, "test", mod+"/...", "--traces", traces); c != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", c, stdout, stderr)
	}
	out, stderr, c := pathkit(t, "check-decisions", mod+"/...", "--traces", traces, "--json", "-p", "2")
	if c != 0 {
		t.Fatalf("check-decisions: exit %d\n%s\n%s", c, out, stderr)
	}
	var dc struct{ Checked, Noticed, Survived int }
	if err := json.Unmarshal([]byte(out), &dc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if dc.Noticed != 4 || dc.Survived != 2 || dc.Checked != 8 {
		t.Errorf("tests in tests/ must be run: %+v\n%s", dc, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Forcing an outcome that every test already takes changes nothing, so it must
// not be listed as a decision "no test noticed".
func TestDecisionCheckSkipsOutcomeEveryTestTakes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the check runs the tests with no network, which needs unshare (Linux)")
	}
	if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("no network namespace here: %v %s", err, out)
	}
	src := abs(t, "../../testdata/fixtures")
	mod := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum"} {
		if data, err := os.ReadFile(filepath.Join(src, f)); err == nil {
			writeFile(t, filepath.Join(mod, f), string(data))
		}
	}
	code, _ := os.ReadFile(filepath.Join(src, "decisions", "decisions.go"))
	writeFile(t, filepath.Join(mod, "decisions", "decisions.go"), string(code))
	tests, _ := os.ReadFile(filepath.Join(src, "decisions", "decisions_test.go"))
	s := string(tests)
	// the activity never fails in the Unchecked tests
	i := strings.Index(s, "func TestUncheckedNoticeFails")
	if i < 0 {
		t.Fatal("fixture changed")
	}
	writeFile(t, filepath.Join(mod, "decisions", "decisions_test.go"), s[:i])

	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, c := pathkit(t, "test", mod+"/...", "--traces", traces); c != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", c, stdout, stderr)
	}
	out, stderr, c := pathkit(t, "check-decisions", mod+"/...", "--traces", traces, "--json", "-p", "2")
	if c != 0 {
		t.Fatalf("check-decisions: exit %d\n%s\n%s", c, out, stderr)
	}
	var dc struct {
		Mutants []struct{ Workflow, Decision, Outcome, Result string }
	}
	if err := json.Unmarshal([]byte(out), &dc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, m := range dc.Mutants {
		if m.Workflow == "decisions.Unchecked" && m.Decision == "When Notify finishes" && m.Outcome == "it succeeds" {
			t.Errorf("forcing the outcome every test takes was checked: %+v", m)
		}
	}
	if len(dc.Mutants) == 0 {
		t.Errorf("nothing was checked:\n%s", out)
	}
}
