package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newModule makes a small Go module in a temp folder and returns it.
func newModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/firstrun\n\ngo 1.26\n"
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// First-run failures: each says what happened and what to do next, and
// exits 1 (a real error, never a coverage problem).
func TestFirstRunFailuresSayWhatToDo(t *testing.T) {
	noMod := t.TempDir()
	if err := os.WriteFile(filepath.Join(noMod, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noWorkflows := newModule(t, map[string]string{"a.go": "package x\n\nfunc Add(a, b int) int { return a + b }\n"})
	broken := newModule(t, map[string]string{"a.go": "package x\n\nfunc Bad() int { return \"s\" }\n"})
	failing := newModule(t, map[string]string{
		"a.go":      "package x\n",
		"a_test.go": "package x\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Fatal(\"no\") }\n",
	})

	orders := absPath(t, pilot+"/orders")
	emptyTraces := t.TempDir()

	tests := []struct {
		name string
		dir  string
		args []string
		want []string // every one must appear in stderr
	}{
		{"no go.mod: analyze", noMod, []string{"analyze", "."}, []string{"no go.mod found", "the folder that holds go.mod"}},
		{"no go.mod: report", noMod, []string{"report"}, []string{"no go.mod found", "the folder that holds go.mod"}},
		{"no go.mod: test", noMod, []string{"test", "./..."}, []string{"no go.mod found", "the folder that holds go.mod"}},
		{"no workflows: analyze", noWorkflows, []string{"analyze", "."}, []string{"has no exported workflow functions", "workflow.Context first and return error last"}},
		{"no workflows: report", noWorkflows, []string{"report"}, []string{"no workflows in scope to measure", "workflow.Context first, error last"}},
		{"build fails: analyze", broken, []string{"analyze", "."}, []string{"package does not compile", `"go build ./..."`}},
		{"build fails: report", broken, []string{"report"}, []string{"package does not compile", "PathKit needs code that compiles"}},
		{"tests fail while recording", failing, []string{"test", "./..."}, []string{"go test failed", "Fix the failing tests"}},
		{"no traces yet", noWorkflows, []string{"report", orders, "--traces", emptyTraces}, []string{"no trace files found", `Run "pathkit test ./..." first`}},
		{"missing argument", noWorkflows, []string{"analyze"}, []string{"missing <file> argument", "for example: pathkit analyze ./..."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
			_, stderr, code := run(t, tt.args...)
			if code != ExitError {
				t.Errorf("exit code %d, want %d; stderr %q", code, ExitError, stderr)
			}
			for _, w := range tt.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr lacks %q:\n%s", w, stderr)
				}
			}
		})
	}
}

// A command run from a sub-folder still finds the project's
// .pathkitrc.json (it is looked up folder by folder up to go.mod), and a
// mistake in it is reported with the file's full path.
func TestConfigIsFoundFromASubFolder(t *testing.T) {
	root := newModule(t, map[string]string{
		".pathkitrc.json": `{"clasification": {}}`,
		"sub/a.go":        "package sub\n",
	})
	t.Chdir(filepath.Join(root, "sub"))
	_, stderr, code := run(t, "analyze", ".")
	if code != ExitError || !strings.Contains(stderr, filepath.Join(root, ".pathkitrc.json")) || !strings.Contains(stderr, `unknown key "clasification"`) {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

// Classification-only options without classification enabled say how to
// switch it on.
func TestBaselineNeedsClassification(t *testing.T) {
	dir := newModule(t, map[string]string{"a.go": "package x\n"})
	t.Chdir(dir)
	for _, flag := range []string{"--baseline", "--write-baseline"} {
		args := []string{"report", flag}
		if flag == "--baseline" {
			args = append(args, "b.json")
		}
		_, stderr, code := run(t, args...)
		if code != ExitError || !strings.Contains(stderr, `set "enabled": true under "classification"`) {
			t.Errorf("%s: code=%d stderr=%q", flag, code, stderr)
		}
	}
}
