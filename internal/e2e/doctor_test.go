package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureCopy makes a copy of the scenarios fixture module to work in.
func fixtureCopy(t *testing.T) string {
	t.Helper()
	src := abs(t, "../../testdata/fixtures")
	dir := t.TempDir()
	copyTree(t, filepath.Join(src, "scenarios"), filepath.Join(dir, "scenarios"))
	for _, f := range []string{"go.mod", "go.sum"} {
		copyTree2(t, filepath.Join(src, f), filepath.Join(dir, f))
	}
	t.Chdir(dir)
	return dir
}

func TestInit(t *testing.T) {
	dir := fixtureCopy(t)
	out, stderr, code := pathkit(t, "init")
	if code != 0 {
		t.Fatalf("init: %d\n%s", code, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".pathkitrc.json"))
	if err != nil || !strings.Contains(string(raw), `"enabled": true`) || !strings.Contains(string(raw), `"default": 80`) {
		t.Fatalf("config not written: %v\n%s", err, raw)
	}
	if !strings.Contains(out, "Workflows PathKit can read: 7") || !strings.Contains(out, "scenarios.Wide") {
		t.Errorf("init should list the workflows:\n%s", out)
	}
	// the file it wrote is valid config
	if _, stderr, code := pathkit(t, "analyze", "./scenarios"); code != 0 {
		t.Errorf("the written config is refused: %d %s", code, stderr)
	}
	// a second run changes nothing
	out, _, code = pathkit(t, "init")
	again, _ := os.ReadFile(filepath.Join(dir, ".pathkitrc.json"))
	if code != 0 || string(again) != string(raw) || !strings.Contains(out, "already exists") || !strings.Contains(out, "every setting PathKit would add") {
		t.Errorf("second init: exit %d\n%s", code, out)
	}
	// an existing config with less: nothing is changed, the missing settings are suggested
	custom := `{"classification": {"enabled": true}}`
	if err := os.WriteFile(filepath.Join(dir, ".pathkitrc.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code = pathkit(t, "init")
	after, _ := os.ReadFile(filepath.Join(dir, ".pathkitrc.json"))
	if code != 0 || string(after) != custom || !strings.Contains(out, "+ classification.targets") || !strings.Contains(out, `+ packages: ["./..."]`) {
		t.Errorf("init with a config: exit %d\n%s\nfile: %s", code, out, after)
	}
}

func TestDoctor(t *testing.T) {
	dir := fixtureCopy(t)
	// no traces yet
	out, _, code := pathkit(t, "doctor")
	if code != 1 || !strings.Contains(out, "PROBLEM  no traces") || !strings.Contains(out, `run "pathkit test ./..."`) {
		t.Fatalf("no traces: exit %d\n%s", code, out)
	}
	if _, stderr, c := pathkit(t, "test", "./..."); c != 0 {
		t.Fatalf("test: %d %s", c, stderr)
	}
	out, _, code = pathkit(t, "doctor")
	// the Events workflow has no test; everything else is fine
	if code != 1 || !strings.Contains(out, "recorded nothing: scenarios.Events") || strings.Contains(out, "PROBLEM  no traces") {
		t.Fatalf("after the tests: exit %d\n%s", code, out)
	}
	// a source change after the traces
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "scenarios", "scenarios.go"), later, later); err != nil {
		t.Fatal(err)
	}
	out, _, _ = pathkit(t, "doctor")
	if !strings.Contains(out, "is newer than the traces") {
		t.Errorf("a changed source file should be reported:\n%s", out)
	}
	// a misspelled key
	if err := os.WriteFile(filepath.Join(dir, ".pathkitrc.json"), []byte(`{"clasification": {"enabled": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code = pathkit(t, "doctor")
	if code != 1 || !strings.Contains(out, `did you mean "classification"?`) {
		t.Errorf("misspelled key: exit %d\n%s", code, out)
	}
	// outside a module
	t.Chdir(t.TempDir())
	if out, _, code := pathkit(t, "doctor"); code != 1 || !strings.Contains(out, "not inside a Go module") {
		t.Errorf("outside a module: exit %d\n%s", code, out)
	}
}

// .pathkit/ must not be committed: doctor says so inside a git repository.
func TestDoctorGitIgnore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := fixtureCopy(t)
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skip("cannot make a git repository")
	}
	out, _, _ := pathkit(t, "doctor")
	if !strings.Contains(out, ".pathkit/ (traces, the overlay and the reports) is not ignored by git") {
		t.Errorf("not ignored should be a problem:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pathkit/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ = pathkit(t, "doctor")
	if strings.Contains(out, "not ignored by git") {
		t.Errorf("ignored should be fine:\n%s", out)
	}
}
