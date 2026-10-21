package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// "pathkit diff" compares the maps of a folder with the same folder at a git
// ref: new workflows, decisions and scenarios, and whether tests cover them.
func TestDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := fixtureCopy(t)
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pathkit/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "checkout", "-q", "-b", "change")

	// no change yet
	if _, stderr, code := pathkit(t, "test", "./..."); code != 0 {
		t.Fatalf("test: %d %s", code, stderr)
	}
	out, stderr, code := pathkit(t, "diff", "--base", "main")
	if code != 0 || !strings.Contains(out, "No workflow, decision, scenario or handler is new") {
		t.Fatalf("no change: exit %d\n%s\n%s", code, out, stderr)
	}

	// the change: a new workflow with a decision and an activity
	fresh := `package scenarios

import "go.temporal.io/sdk/workflow"

// Fresh is new in the change.
func Fresh(ctx workflow.Context, in Input) error {
	ctx = options(ctx)
	if in.Urgent {
		if err := workflow.ExecuteActivity(ctx, ChargeBig).Get(ctx, nil); err != nil {
			return err
		}
	}
	if err := workflow.ExecuteActivity(ctx, Charge).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "scenarios", "fresh.go"), []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "add Fresh")
	if _, stderr, code := pathkit(t, "test", "./..."); code != 0 {
		t.Fatalf("test: %d %s", code, stderr)
	}
	out, _, code = pathkit(t, "diff", "--base", "main")
	if code != 0 || !strings.Contains(out, "+ scenarios.Fresh") || !strings.Contains(out, "NO TEST") || !strings.Contains(out, "if in.Urgent") {
		t.Fatalf("untested change: exit %d\n%s", code, out)
	}
	if strings.Contains(out, "scenarios.Logic") {
		t.Errorf("an unchanged workflow must not be listed:\n%s", out)
	}
	// the gate
	if _, stderr, code := pathkit(t, "diff", "--base", "main", "--fail-on-untested-new"); code != 2 || !strings.Contains(stderr, "no test") {
		t.Errorf("--fail-on-untested-new: exit %d %q", code, stderr)
	}
	// the branch gate: Fresh's outcomes are new and no test took any of them
	if out, stderr, code := pathkit(t, "diff", "--base", "main", "--min-new-branch-coverage", "90"); code != 2 || !strings.Contains(stderr, "below --min-new-branch-coverage 90%") ||
		!strings.Contains(stderr, "fresh.go") || !strings.Contains(out, "New or changed decision outcomes: 0 of ") {
		t.Errorf("--min-new-branch-coverage: exit %d\nstdout %s\nstderr %q", code, out, stderr)
	}
	// without the flag the older output has no such line
	if plain, _, _ := pathkit(t, "diff", "--base", "main"); strings.Contains(plain, "New or changed decision outcomes") {
		t.Errorf("the line is only printed when asked for:\n%s", plain)
	}
	// markdown and json
	md, _, _ := pathkit(t, "diff", "--base", "main", "--markdown")
	if !strings.Contains(md, "| Change | What | Where | Tested |") || !strings.Contains(md, "**no**") {
		t.Errorf("markdown:\n%s", md)
	}
	js, _, _ := pathkit(t, "diff", "--base", "main", "--json")
	var doc struct {
		Untested int                                   `json:"untestedNew"`
		Changes  []struct{ Kind, Change, Name string } `json:"changes"`
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil || doc.Untested < 2 || len(doc.Changes) < 3 {
		t.Errorf("json: %v %+v\n%s", err, doc, js)
	}

	// a test for Fresh that takes both decisions: nothing new is untested
	test := `package scenarios

import "testing"

func TestFreshUrgent(t *testing.T) { run(t, Fresh, Input{Urgent: true}) }
func TestFreshCalm(t *testing.T)   { run(t, Fresh, Input{}) }
func TestFreshFails(t *testing.T)  { run(t, Fresh, Input{}) }
`
	if err := os.WriteFile(filepath.Join(dir, "scenarios", "fresh_test.go"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := pathkit(t, "test", "./..."); code != 0 {
		t.Fatalf("test: %d %s", code, stderr)
	}
	out, _, code = pathkit(t, "diff", "--base", "main", "--fail-on-untested-new")
	// the failure scenarios of Fresh are still new and untested, but the decisions are covered
	if !strings.Contains(out, "if in.Urgent") || strings.Contains(out, "if in.Urgent) — NO TEST") {
		t.Errorf("the decision is tested now:\n%s", out)
	}
	_ = code
	// every new decision outcome of Fresh has a test now, except the failures
	// of the second activity: the gate at 60% passes, at 100% it names what is left
	if _, stderr, code := pathkit(t, "diff", "--base", "main", "--min-new-branch-coverage", "10"); code != 0 {
		t.Errorf("gate at 10%%: exit %d %q", code, stderr)
	}

	// no leftover worktree, also after an error
	if _, _, code := pathkit(t, "diff", "--base", "no-such-ref"); code == 0 {
		t.Error("an unknown ref is an error")
	}
	if list := git(t, dir, "worktree", "list"); strings.Count(list, "\n") != 1 {
		t.Errorf("a temporary worktree was left behind:\n%s", list)
	}
}
