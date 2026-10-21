package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Every scaffold PathKit writes for the top items compiles, and when it runs
// it covers the scenario it was written for (PathKit shows it covered). An
// existing file is not overwritten.
func TestScaffoldsCompileAndCover(t *testing.T) {
	src := abs(t, "../../testdata/fixtures")
	dir := t.TempDir()
	copyTree(t, filepath.Join(src, "scenarios"), filepath.Join(dir, "scenarios"))
	for _, f := range []string{"go.mod", "go.sum"} {
		copyTree2(t, filepath.Join(src, f), filepath.Join(dir, f))
	}
	t.Chdir(dir)
	traces := filepath.Join(dir, "tr")
	if _, stderr, code := pathkit(t, "test", "./scenarios/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: %d\n%s", code, stderr)
	}
	before := summarizeReport(t, "./scenarios", traces, "")
	type item struct{ wf, id string }
	var items []item
	for _, x := range before["testsToWriteFirst"].([]any) {
		m := x.(map[string]any)
		if strings.HasPrefix(m["id"].(string), "sc:") {
			items = append(items, item{m["workflow"].(string), m["id"].(string)})
		}
	}
	if len(items) < 6 {
		t.Fatalf("only %d scenarios in the list to write first: %v", len(items), before["testsToWriteFirst"])
	}
	if _, stderr, code := pathkit(t, "scaffold", "--top", "10", "--out", "./scenarios", "--traces", traces, "./scenarios"); code != 0 {
		t.Fatalf("scaffold: %d\n%s", code, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "scenarios", "pathkit_scaffold_*_test.go"))
	if len(files) == 0 {
		t.Fatal("no scaffold files written")
	}
	// the input is set so that the decisions go the way the scenario goes
	if raw, err := os.ReadFile(filepath.Join(dir, "scenarios", "pathkit_scaffold_Different_test.go")); err != nil ||
		!strings.Contains(string(raw), "in.Big = true") || !strings.Contains(string(raw), "in.Big = false") || !strings.Contains(string(raw), "env.OnActivity(ChargeBig, mock.Anything)") {
		t.Errorf("the Different scaffold does not set the input or mock the activity: %v\n%s", err, raw)
	}
	// writing again must not overwrite
	if _, stderr, code := pathkit(t, "scaffold", "--top", "10", "--out", "./scenarios", "--traces", traces, "./scenarios"); code == 0 || !strings.Contains(stderr, "already exists") {
		t.Errorf("second scaffold: exit %d, %q", code, stderr)
	}
	vet := exec.Command("go", "vet", "./scenarios")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("the scaffolds do not compile: %v\n%s", err, out)
	}
	traces2 := filepath.Join(dir, "tr2")
	if _, stderr, code := pathkit(t, "test", "./scenarios/...", "--traces", traces2); code != 0 {
		t.Fatalf("running the scaffolds: %d\n%s", code, stderr)
	}
	after := summarizeReport(t, "./scenarios", traces2, "")
	covered := map[item]bool{}
	for _, w := range after["workflows"].([]any) {
		wm := w.(map[string]any)
		for _, s := range wm["scenarios"].(map[string]any)["list"].([]any) {
			sm := s.(map[string]any)
			covered[item{wm["name"].(string), sm["id"].(string)}] = sm["covered"] == true
		}
	}
	for _, it := range items {
		if !covered[it] {
			t.Errorf("the scaffold for %s of %s does not cover it", it.id, it.wf)
		}
	}
}

func copyTree2(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The scaffolds of the workflows whose decisions read the input, what an
// activity gives back and what a signal carries: most of them must reach the
// scenario they are written for with no edit (milestone M11, L2). The share is
// logged; it must be at least 75% (the one that cannot be solved is the Opaque workflow, whose TODO says so, and a scenario of a wait that ends on a timer).
func TestScaffoldsSetInputsFromTheFacts(t *testing.T) {
	src := abs(t, "../../testdata/fixtures")
	dir := t.TempDir()
	copyTree(t, filepath.Join(src, "scaffolds"), filepath.Join(dir, "scaffolds"))
	for _, f := range []string{"go.mod", "go.sum"} {
		copyTree2(t, filepath.Join(src, f), filepath.Join(dir, f))
	}
	t.Chdir(dir)
	traces := filepath.Join(dir, "tr")
	if _, stderr, code := pathkit(t, "test", "./scaffolds/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: %d\n%s", code, stderr)
	}
	before := summarizeReport(t, "./scaffolds", traces, "")
	type item struct{ wf, id string }
	var items []item
	for _, x := range before["testsToWriteFirst"].([]any) {
		m := x.(map[string]any)
		if strings.HasPrefix(m["id"].(string), "sc:") {
			items = append(items, item{m["workflow"].(string), m["id"].(string)})
		}
	}
	if len(items) < 8 {
		t.Fatalf("only %d scenarios to write first: %v", len(items), before["testsToWriteFirst"])
	}
	if _, stderr, code := pathkit(t, "scaffold", "--top", "40", "--out", "./scaffolds", "--traces", traces, "./scaffolds"); code != 0 {
		t.Fatalf("scaffold: %d\n%s", code, stderr)
	}
	vet := exec.Command("go", "vet", "./scaffolds")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("the scaffolds do not compile: %v\n%s", err, out)
	}
	// what PathKit cannot work out is a TODO that names the condition and the value it needs
	if raw, err := os.ReadFile(filepath.Join(dir, "scaffolds", "pathkit_scaffold_Opaque_test.go")); err != nil ||
		!strings.Contains(string(raw), "// TODO: set the input so that `req.Describe() == \"RESET!\"` is true (it reads something other than a field of the input)") {
		t.Errorf("the Opaque scaffold should say what it could not solve: %v\n%s", err, raw)
	}
	// the facts set the input, an activity's result and a signal's payload
	for file, wants := range map[string][]string{
		"Route":  {`req.Type = "RESET"`, `req.Note = func() *string { v := *new(string); return &v }()`, `req.Items = []string{}`, `req.Priority = 5`, `req.Note = nil`},
		"Review": {`vOut := *new(Verdict)`, `vOut.Status = "APPROVED"`, `.Return(vOut, nil)`, `vOut.Score = 51`},
		"Kinds":  {`req.Payload = "x"`, `req.Payload = map[string]interface{}{}`, `req.Payload = float64(1)`},
		"Wait":   {`dPayload.Approved = true`, `env.SignalWorkflow("decide", dPayload)`},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, "scaffolds", "pathkit_scaffold_"+file+"_test.go"))
		if err != nil {
			t.Errorf("no scaffold for %s: %v", file, err)
			continue
		}
		for _, w := range wants {
			if !strings.Contains(string(raw), w) {
				t.Errorf("the %s scaffold lacks %q:\n%s", file, w, raw)
			}
		}
	}
	traces2 := filepath.Join(dir, "tr2")
	if _, stderr, code := pathkit(t, "test", "./scaffolds/...", "--traces", traces2); code != 0 {
		t.Logf("running the scaffolds: %d (a scaffold may fail on its own TODO)\n%s", code, stderr)
	}
	after := summarizeReport(t, "./scaffolds", traces2, "")
	covered := map[item]bool{}
	for _, w := range after["workflows"].([]any) {
		wm := w.(map[string]any)
		for _, s := range wm["scenarios"].(map[string]any)["list"].([]any) {
			sm := s.(map[string]any)
			covered[item{wm["name"].(string), sm["id"].(string)}] = sm["covered"] == true
		}
	}
	reached := 0
	for _, it := range items {
		if covered[it] {
			reached++
		} else {
			t.Logf("not reached: %s of %s", it.id, it.wf)
		}
	}
	t.Logf("SCAFFOLDS: %d of %d reach their scenario with no edit", reached, len(items))
	if reached*100 < len(items)*75 {
		t.Errorf("only %d of %d scaffolds reach their scenario", reached, len(items))
	}
}
