package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// "return fut.Get(...)" and "return workflow.Sleep(...)" are decisions: the
// call fails or succeeds. A test of one outcome no longer covers the other.
func TestDirectReturnOfACallIsADecision(t *testing.T) {
	fixture := abs(t, "../../testdata/fixtures/returncall")
	t.Chdir(t.TempDir())
	traces := filepath.Join(t.TempDir(), "traces")
	cfg := filepath.Join(t.TempDir(), "step.json")
	if err := os.WriteFile(cfg, []byte(`{"classification": {"direct_returns": "step"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := pathkit(t, "test", fixture+"/...", "--traces", traces, "--config", cfg); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	doc := summarizeReport(t, fixture, traces, cfg)
	for name, want := range map[string][2]float64{
		"returncall.ReturnGet":       {2, 2}, // failure and success, both tested
		"returncall.ReturnSleep":     {3, 1}, // return early, then the timer fails or succeeds; one tested
		"returncall.ReturnWithValue": {2, 1},
	} {
		w := workflowNamed(t, doc, name)
		p := w["paths"].(map[string]any)
		if p["total"].(float64) != want[0] || p["covered"].(float64) != want[1] {
			t.Errorf("%s: %v of %v paths covered, want %v of %v", name, p["covered"], p["total"], want[1], want[0])
		}
	}
	if tr := doc["traces"].(map[string]any); tr["unmatched"].(float64) != 0 {
		t.Errorf("traces: %v", tr)
	}
}

// By default a direct return stays one path, as before, and is listed in the
// "could not fully read" panel with what to set.
func TestDirectReturnIsReportedByDefault(t *testing.T) {
	fixture := abs(t, "../../testdata/fixtures/returncall")
	t.Chdir(t.TempDir())
	traces := filepath.Join(t.TempDir(), "traces")
	if _, stderr, code := pathkit(t, "test", fixture+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	doc := summarizeReport(t, fixture, traces, "")
	if p := workflowNamed(t, doc, "returncall.ReturnGet")["paths"].(map[string]any); p["total"].(float64) != 1 {
		t.Errorf("ReturnGet has %v paths by default, want the single path it always had", p["total"])
	}
	listed := map[string]bool{}
	for _, u := range doc["unread"].([]any) {
		m := u.(map[string]any)
		if m["kind"] == "result returned directly" {
			listed[m["workflow"].(string)] = true
		}
	}
	for _, name := range []string{"returncall.ReturnGet", "returncall.ReturnSleep", "returncall.ReturnWithValue"} {
		if !listed[name] {
			t.Errorf("%s is not listed as returning a call directly", name)
		}
	}
	// Near miss: a return of a helper's result is not one of these shapes.
	if listed["returncall.ReturnHelper"] {
		t.Errorf("ReturnHelper is listed")
	}
}
