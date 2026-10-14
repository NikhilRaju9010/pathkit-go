package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two workflows with the same package name and the same function name in
// different folders (a/dup.Work and b/dup.Work) must never be mixed up:
// not their traces, not their stable ids, not an override written for one
// of them. The two first paths here have the very same stable id on
// purpose, so a mix-up would show.
func TestSameWorkflowNameInTwoFolders(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/samename")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", root+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if len(files) != 2 || !strings.Contains(joined, "a_dup.Work.") || !strings.Contains(joined, "b_dup.Work.") {
		t.Fatalf("want one trace per workflow with a safe file name, got %v", names)
	}

	type pathInfo struct {
		StableID string  `json:"stableId"`
		Covered  bool    `json:"covered"`
		Bucket   string  `json:"bucket"`
		Number   float64 `json:"number"`
	}
	type workflow struct {
		Name     string     `json:"name"`
		PathList []pathInfo `json:"pathList"`
	}
	type report struct {
		Workflows []workflow `json:"workflows"`
		Traces    struct {
			Counted, Stale, Unmatched, UnknownWorkflow int
		} `json:"traces"`
	}
	run := func(t *testing.T, args ...string) (report, string, int) {
		t.Helper()
		out, errOut, code := pathkit(t, append([]string{"report", root + "/...", "--traces", traces, "--json"}, args...)...)
		var r report
		if code == 0 {
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		}
		return r, errOut, code
	}
	byName := func(r report, name string) workflow {
		for _, w := range r.Workflows {
			if w.Name == name {
				return w
			}
		}
		t.Fatalf("no workflow %q in %+v", name, r.Workflows)
		return workflow{}
	}
	coveredCount := func(w workflow) int {
		n := 0
		for _, p := range w.PathList {
			if p.Covered {
				n++
			}
		}
		return n
	}

	// 1. Each workflow gets exactly its own trace.
	r, errOut, code := run(t)
	if code != 0 {
		t.Fatalf("report: exit %d\n%s", code, errOut)
	}
	a, b := byName(r, "a/dup.Work"), byName(r, "b/dup.Work")
	if len(a.PathList) != 2 || len(b.PathList) != 3 {
		t.Fatalf("paths: a %d, b %d; want 2 and 3", len(a.PathList), len(b.PathList))
	}
	if coveredCount(a) != 1 || coveredCount(b) != 1 || !a.PathList[0].Covered || b.PathList[0].Covered {
		t.Errorf("coverage mixed up: a=%+v b=%+v", a.PathList, b.PathList)
	}
	if r.Traces.Counted != 2 || r.Traces.Stale != 0 || r.Traces.Unmatched != 0 || r.Traces.UnknownWorkflow != 0 {
		t.Errorf("traces: %+v", r.Traces)
	}
	if a.PathList[0].StableID != b.PathList[0].StableID {
		t.Fatalf("the test needs equal stable ids for the two first paths: %s vs %s", a.PathList[0].StableID, b.PathList[0].StableID)
	}

	// 2. An override for b's first path touches b only, although a's first
	// path has the same stable id (and is covered: a mix-up would warn).
	cfg := filepath.Join(t.TempDir(), ".pathkitrc.json")
	pk := `"packages": [` + jsonText(t, root+"/...") + `], `
	body := `{` + pk + `"classification": {"enabled": true, "paths": {"mark_untestable": [{"workflow": "b/dup.Work", "stable_id": ` +
		jsonText(t, b.PathList[0].StableID) + `, "reason": "only b can never see a positive number"}]}}}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, errOut, code = run(t, "--config", cfg)
	if code != 0 {
		t.Fatalf("report with the override: exit %d\n%s", code, errOut)
	}
	a, b = byName(r, "a/dup.Work"), byName(r, "b/dup.Work")
	if b.PathList[0].Bucket != "cannot-be-tested" || a.PathList[0].Bucket != "testable" {
		t.Errorf("override mixed up: a=%s b=%s", a.PathList[0].Bucket, b.PathList[0].Bucket)
	}
	if strings.Contains(errOut, "a/dup.Work") {
		t.Errorf("a warning mentions a/dup.Work, which has no override:\n%s", errOut)
	}

	// 3. A short name that matches both is an error that names both; the
	// qualified form works in include, exclude and overrides.
	short := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(short, []byte(`{`+pk+`"workflows": {"exclude": [{"name": "dup.Work", "reason": "x"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code = run(t, "--config", short); code == 0 || !strings.Contains(errOut, "a/dup.Work") || !strings.Contains(errOut, "b/dup.Work") {
		t.Errorf("an ambiguous name must be an error naming both: exit %d\n%s", code, errOut)
	}
	qualified := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(qualified, []byte(`{`+pk+`"workflows": {"exclude": [{"name": "a/dup.Work", "reason": "x"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, errOut, code = run(t, "--config", qualified)
	if code != 0 || len(r.Workflows) != 1 || r.Workflows[0].Name != "b/dup.Work" {
		t.Errorf("excluding a/dup.Work must leave only b/dup.Work: exit %d, %+v\n%s", code, r.Workflows, errOut)
	}
}

// Names must not depend on which folder a command is given. With a config,
// the names are decided over the config's packages: recording one folder
// and reporting another, or excluding by the qualified name while looking
// at one folder only, still means the same workflow.
func TestSameWorkflowNameDoesNotDependOnTheFolderGiven(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/samename")
	t.Chdir(t.TempDir())
	cfg := filepath.Join(t.TempDir(), ".pathkitrc.json")
	body := `{"packages": [` + jsonText(t, root+"/...") + `], "workflows": {"exclude": [{"name": "a/dup.Work", "reason": "needs a sandbox"}]}}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Looking at a/ only: the workflow is still a/dup.Work and still excluded.
	out, errOut, code := pathkit(t, "analyze", root+"/a/...", "--config", cfg, "--summary")
	if code != 1 || strings.Contains(out, "Workflow: ") || !strings.Contains(out, "a/dup.Work: needs a sandbox") || !strings.Contains(errOut, "every workflow is excluded") {
		t.Errorf("analyze a/ with a/dup.Work excluded: exit %d\n%s\n%s", code, out, errOut)
	}
	// Looking at b/ only: the name is b/dup.Work, not dup.Work.
	out, errOut, code = pathkit(t, "analyze", root+"/b/...", "--config", cfg, "--summary")
	if code != 0 || !strings.Contains(out, "Workflow: b/dup.Work") {
		t.Errorf("analyze b/: exit %d\n%s\n%s", code, out, errOut)
	}
	// Record only a's folder, report everything: the trace is a/dup.Work.
	traces := t.TempDir()
	noExclude := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(noExclude, []byte(`{"packages": [`+jsonText(t, root+"/...")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := pathkit(t, "test", root+"/a/...", "--config", noExclude, "--traces", traces); code != 0 {
		t.Fatalf("test a/: exit %d\n%s", code, errOut)
	}
	files, _ := filepath.Glob(filepath.Join(traces, "a_dup.Work.*.trace.json"))
	if len(files) != 1 {
		t.Fatalf("want one a_dup.Work trace, got %v", files)
	}
	out, errOut, code = pathkit(t, "report", root+"/...", "--config", noExclude, "--traces", traces, "--summary")
	if code != 0 || !strings.Contains(out, "a/dup.Work  1/2 paths") || strings.Contains(out, "unknown workflow ") && !strings.Contains(out, "0 unknown workflow") {
		t.Errorf("report after recording a/ only: exit %d\n%s\n%s", code, out, errOut)
	}
}
