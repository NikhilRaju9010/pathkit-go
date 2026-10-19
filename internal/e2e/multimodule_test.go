package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule makes a small Go module with one workflow and one test of it.
func writeModule(t *testing.T, root, name, module string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := abs(t, "../../testdata/constructs")
	for _, f := range []string{"go.sum"} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := strings.Replace(string(mod), "module example.com/constructs", "module "+module, 1)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	code := "package " + name + "\n\nimport (\n\t\"time\"\n\n\t\"go.temporal.io/sdk/workflow\"\n)\n\n// Run waits once.\nfunc Run(ctx workflow.Context, fast bool) error {\n\tif fast {\n\t\treturn nil\n\t}\n\tif err := workflow.Sleep(ctx, time.Millisecond); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
	test := "package " + name + "\n\nimport (\n\t\"testing\"\n\n\t\"go.temporal.io/sdk/testsuite\"\n)\n\nfunc TestRun(t *testing.T) {\n\tvar s testsuite.WorkflowTestSuite\n\tenv := s.NewTestWorkflowEnvironment()\n\tenv.ExecuteWorkflow(Run, false)\n\tif !env.IsWorkflowCompleted() {\n\t\tt.Fatal(\"not completed\")\n\t}\n}\n"
	for f, body := range map[string]string{name + ".go": code, name + "_test.go": test} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A folder that holds several Go modules (and no go.mod of its own) is read
// module by module, in one combined report with the module on each workflow;
// --module limits it to one.
func TestFolderWithSeveralModules(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "alpha", "example.com/alpha")
	writeModule(t, root, "beta", "example.com/beta")
	t.Chdir(t.TempDir())
	traces := filepath.Join(t.TempDir(), "traces")
	if _, stderr, code := pathkit(t, "test", root, "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	doc := summarizeReport(t, root, traces, "")
	got := map[string]string{}
	for _, w := range doc["workflows"].([]any) {
		m := w.(map[string]any)
		got[m["name"].(string)], _ = m["module"].(string)
	}
	if len(got) != 2 || got["alpha.Run"] != "example.com/alpha" || got["beta.Run"] != "example.com/beta" {
		t.Fatalf("workflows and modules: %v", got)
	}
	if tr := doc["traces"].(map[string]any); tr["counted"].(float64) != 2 {
		t.Errorf("traces: %v", tr)
	}
	out, stderr, code := pathkit(t, "report", root, "--traces", traces, "--json", "--module", "beta")
	if code != 0 {
		t.Fatalf("--module: exit %d\n%s", code, stderr)
	}
	if strings.Contains(out, "alpha.Run") || !strings.Contains(out, "beta.Run") {
		t.Errorf("--module beta should keep only beta:\n%.400s", out)
	}
	// A module that is not there is refused, naming the ones that are.
	if _, stderr, code := pathkit(t, "report", root, "--traces", traces, "--module", "gamma"); code == 0 || !strings.Contains(stderr, "alpha, beta") {
		t.Errorf("--module gamma: exit %d %q", code, stderr)
	}
	// Near miss: a folder inside one module is unchanged (no module field).
	one := summarizeReport(t, filepath.Join(root, "alpha"), traces, "")
	for _, w := range one["workflows"].([]any) {
		if _, has := w.(map[string]any)["module"]; has {
			t.Errorf("a single module's workflow carries a module field")
		}
	}
}
