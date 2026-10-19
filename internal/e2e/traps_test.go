package e2e

import (
	"path/filepath"
	"testing"
)

// An activity chosen through a variable is not "the same behaviour" on two
// paths, and a workflow without any decision still gets a scaffold.
func TestScenarioTraps(t *testing.T) {
	src := abs(t, "../../testdata/fixtures")
	dir := t.TempDir()
	copyTree(t, filepath.Join(src, "scenariotraps"), filepath.Join(dir, "scenariotraps"))
	for _, f := range []string{"go.mod", "go.sum"} {
		copyTree2(t, filepath.Join(src, f), filepath.Join(dir, f))
	}
	t.Chdir(dir)
	cfg := writeConfig(t, `{"classification": {"enabled": true}}`)
	traces := filepath.Join(dir, "tr")
	if _, stderr, code := pathkit(t, "test", "./scenariotraps/...", "--traces", traces); code != 0 {
		t.Fatalf("test: %d %s", code, stderr)
	}
	doc := summarizeReport(t, "./scenariotraps", traces, cfg)
	v := workflowNamed(t, doc, "scenariotraps.Variable")
	for _, p := range v["pathList"].([]any) {
		if p.(map[string]any)["sameBehaviourAs"] != nil {
			t.Errorf("two paths that may call different activities were called the same: %v", p)
		}
	}
	// Straight has one path with no step in it; its scaffold must still be written
	if _, stderr, code := pathkit(t, "scaffold", "--top", "5", "--out", "./scenariotraps", "--traces", traces, "./scenariotraps"); code != 0 {
		t.Fatalf("scaffold: %d\n%s", code, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "scenariotraps", "pathkit_scaffold_Straight_test.go"))
	if len(files) != 1 {
		t.Errorf("no scaffold for the workflow without decisions: %v", files)
	}
}
