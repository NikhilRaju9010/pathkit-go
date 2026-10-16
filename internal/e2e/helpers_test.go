package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Workflows that call helper functions are recorded through the helpers: every
// run's trace, with the decisions made inside the helpers, lands on a path.
func TestHelperTracesMatch(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/helperrun")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", root, "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	if len(files) != 21 {
		t.Fatalf("%d traces, want one for each of the 21 tests", len(files))
	}
	markers := 0
	for _, f := range files {
		data, _ := os.ReadFile(f)
		var tr struct {
			SchemaVersion int      `json:"schemaVersion"`
			Steps         []string `json:"steps"`
			Status        string   `json:"status"`
		}
		if err := json.Unmarshal(data, &tr); err != nil {
			t.Fatal(err)
		}
		if tr.SchemaVersion != 2 || tr.Status != "complete" {
			t.Errorf("%s: schemaVersion %d, status %s", filepath.Base(f), tr.SchemaVersion, tr.Status)
		}
		opens, closes := 0, 0
		for _, s := range tr.Steps {
			switch {
			case strings.HasPrefix(s, ">"):
				opens++
			case strings.HasPrefix(s, "<"):
				closes++
			}
		}
		if opens != closes {
			t.Errorf("%s: %d helper entries and %d exits in %v", filepath.Base(f), opens, closes, tr.Steps)
		}
		markers += opens
	}
	if markers == 0 {
		t.Error("no trace went through a helper")
	}

	out, errOut, code := pathkit(t, "report", root, "--traces", traces, "--json")
	if code != 0 {
		t.Fatalf("report: exit %d\n%s", code, errOut)
	}
	var r struct {
		Workflows []struct {
			Name     string `json:"name"`
			PathList []struct {
				Covered bool `json:"covered"`
			} `json:"pathList"`
		} `json:"workflows"`
		Traces struct {
			Counted, Unmatched int
		} `json:"traces"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Traces.Counted != 21 || r.Traces.Unmatched != 0 {
		t.Errorf("traces counted %d, unmatched %d; want 21 and 0\n%s", r.Traces.Counted, r.Traces.Unmatched, errOut)
	}
	// covered/total paths, by the tests in helperrun_test.go
	want := map[string][2]int{
		"helperrun.BranchHelper": {4, 5}, "helperrun.ClosureHelper": {2, 3}, "helperrun.InLoop": {2, 3},
		"helperrun.LoopHelper": {3, 3}, "helperrun.MethodHelper": {2, 3}, "helperrun.OneHelper": {2, 2},
		"helperrun.PureHelper": {1, 1}, "helperrun.ThreeDeep": {3, 3}, "helperrun.TwoCalls": {2, 3},
	}
	for _, w := range r.Workflows {
		covered := 0
		for _, p := range w.PathList {
			if p.Covered {
				covered++
			}
		}
		if got, ok := want[w.Name]; !ok || got != [2]int{covered, len(w.PathList)} {
			t.Errorf("%s: covered %d of %d, want %v", w.Name, covered, len(w.PathList), want[w.Name])
		}
	}
}

// Traces recorded by an older PathKit are not read, and the report says so
// in one line with what to do.
func TestOlderTracesAreRefused(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/helperrun")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	old := `{"schemaVersion":1,"tool":"pathkit-go","workflow":"helperrun.OneHelper","functionHash":"x","status":"complete","steps":["J1.failure"]}`
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(traces, n+".trace.json"), []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, errOut, code := pathkit(t, "coverage", root, "--traces", traces)
	if code != 0 {
		t.Fatalf("coverage: exit %d\n%s", code, errOut)
	}
	if n := strings.Count(errOut, "traces are from an older PathKit"); n != 1 {
		t.Errorf("the message appears %d times, want once:\n%s", n, errOut)
	}
	if !strings.Contains(errOut, `re-run "pathkit test"`) || !strings.Contains(errOut, "one of 2 trace files") {
		t.Errorf("message does not say how many and what to do:\n%s", errOut)
	}
}
