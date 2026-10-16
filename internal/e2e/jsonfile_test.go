package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeJSONFile(t *testing.T, path string, m map[string]any) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// New traces say which package the workflow is in; an old trace without
// that field still loads; and a trace that names another package is never
// matched to a workflow of the same name, even with --allow-stale.
func TestTracePackageField(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/samename")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if _, stderr, code := pathkit(t, "test", root+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	if len(files) != 2 {
		t.Fatalf("%d traces", len(files))
	}
	want := map[string]string{"a_dup.Work": "example.com/fixtures/samename/a/dup", "b_dup.Work": "example.com/fixtures/samename/b/dup"}
	for _, f := range files {
		m := readJSONFile(t, f)
		got, _ := m["package"].(string)
		for k, v := range want {
			if strings.Contains(filepath.Base(f), k+".") && got != v {
				t.Errorf("%s: package = %q, want %q", filepath.Base(f), got, v)
			}
		}
	}

	counts := func(extra ...string) (counted, unknown float64, stderr string) {
		t.Helper()
		out, errOut, code := pathkit(t, append([]string{"report", root + "/...", "--traces", traces, "--json"}, extra...)...)
		if code != 0 {
			t.Fatalf("report: exit %d\n%s", code, errOut)
		}
		var r struct {
			Traces struct{ Counted, UnknownWorkflow float64 } `json:"traces"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r.Traces.Counted, r.Traces.UnknownWorkflow, errOut
	}
	if c, u, _ := counts(); c != 2 || u != 0 {
		t.Fatalf("new traces: counted %v, unknown %v", c, u)
	}

	// An old file: no package field. It must load and count as before.
	for _, f := range files {
		m := readJSONFile(t, f)
		delete(m, "package")
		writeJSONFile(t, f, m)
	}
	if c, u, _ := counts(); c != 2 || u != 0 {
		t.Errorf("old traces: counted %v, unknown %v", c, u)
	}

	// a's trace now claims b's package: it must not be counted for a.
	for _, f := range files {
		if strings.Contains(filepath.Base(f), "a_dup.Work.") {
			m := readJSONFile(t, f)
			m["package"] = "example.com/fixtures/samename/b/dup"
			writeJSONFile(t, f, m)
		}
	}
	c, u, errOut := counts()
	if c != 1 || u != 1 || !strings.Contains(errOut, "recorded for the workflow of that name in package example.com/fixtures/samename/b/dup") {
		t.Errorf("wrong package: counted %v, unknown %v\n%s", c, u, errOut)
	}
}

// The scope gap: a trace recorded when only a/dup was loaded is named
// "dup.Work". Reading it against b alone, the names are equal. The package
// field is what tells them apart (the code differs, so without
// --allow-stale it is "stale"; with it, the package decides).
func TestSameNameWithoutConfigUsesPackage(t *testing.T) {
	root := abs(t, "../../testdata/fixtures/samename")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	if _, stderr, code := pathkit(t, "test", root+"/a/dup", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	out, errOut, code := pathkit(t, "report", root+"/b/dup", "--traces", traces, "--json", "--allow-stale")
	if code != 0 {
		t.Fatalf("report: exit %d\n%s", code, errOut)
	}
	var r struct {
		Traces struct{ Counted, UnknownWorkflow float64 } `json:"traces"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Traces.Counted != 0 || r.Traces.UnknownWorkflow != 1 {
		t.Errorf("a's trace was used for b: %+v\n%s", r.Traces, errOut)
	}
}

// Rules with a "without" part, the ambiguity error, and a trace that fits
// no path, through the real command.
func TestRulesWithoutAmbiguityAndUnexplainedTrace(t *testing.T) {
	pkg := abs(t, "../../testdata/fixtures/rulewithout")
	dir := t.TempDir()
	t.Chdir(dir)
	traces := filepath.Join(dir, "traces")
	if err := os.MkdirAll(traces, 0o755); err != nil {
		t.Fatal(err)
	}
	const listWF, twiceWF = "rulewithout.ListWorkflow", "rulewithout.TwiceWorkflow"
	writeJSONFile(t, filepath.Join(traces, "list.trace.json"), map[string]any{
		"schemaVersion": 2, "tool": "pathkit-go", "workflow": listWF, "functionHash": "0000000000000000",
		"status": "complete", "steps": []string{"J1.nowhere"}, "workflowId": "w", "runId": "r", "recordedAt": "2026-10-12T00:00:00Z",
	})

	config := func(rules []map[string]any, explained []map[string]any) string {
		cl := map[string]any{"enabled": true, "rules": rules}
		if explained != nil {
			cl["explained_traces"] = explained
		}
		path := filepath.Join(dir, ".pathkitrc.json")
		writeJSONFile(t, path, map[string]any{"packages": []string{pkg}, "traces": traces, "classification": cl})
		return path
	}
	listRule := map[string]any{"id": "list-skipped", "workflow": listWF, "label": "same-list-loops", "impossible": true, "reason": "one future per item",
		"when": []string{"for range items --exit", "for range futures --iterate"}, "without": []string{"for range items --iterate"}}
	anyRule := map[string]any{"id": "twice-any", "workflow": twiceWF, "label": "contradictory", "impossible": true, "reason": "same condition twice",
		"when": []string{"any: if a || b --false"}}

	// 1. A plain text that fits two decisions is an error, with the way out.
	plain := map[string]any{"id": "twice-plain", "workflow": twiceWF, "label": "contradictory", "impossible": true, "reason": "r", "when": []string{"if a || b --false"}}
	config([]map[string]any{plain}, nil)
	_, errOut, code := pathkit(t, "report", pkg, "--traces", traces, "--allow-stale", "--json")
	if code != 1 || !strings.Contains(errOut, "matches 2 different decisions") || !strings.Contains(errOut, "any: if a || b --false") || !strings.Contains(errOut, "twice-plain") {
		t.Fatalf("ambiguity: exit %d\n%s", code, errOut)
	}

	type bucket struct{ Paths float64 }
	type unexplained struct {
		StepsID      string `json:"stepsId"`
		Acknowledged bool   `json:"acknowledged"`
		Reason       string `json:"reason"`
	}
	type workflow struct {
		Name           string `json:"name"`
		Classification struct {
			Buckets struct {
				CannotBeTested bucket `json:"cannotBeTested"`
				NeedsReview    bucket `json:"needsReview"`
			} `json:"buckets"`
		} `json:"classification"`
		Unexplained []unexplained `json:"unexplainedTraces"`
	}
	type report struct {
		Workflows []workflow `json:"workflows"`
	}
	run := func(t *testing.T) (report, string) {
		t.Helper()
		out, errOut, code := pathkit(t, "report", pkg, "--traces", traces, "--allow-stale", "--json")
		if code != 0 {
			t.Fatalf("report: exit %d\n%s", code, errOut)
		}
		var r report
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r, errOut
	}
	find := func(r report, name string) workflow {
		for _, w := range r.Workflows {
			if w.Name == name {
				return w
			}
		}
		t.Fatalf("no %s", name)
		return workflow{}
	}

	// 2. "without" and "any:" work through the real command, and the
	// trace that fits no path puts List's claims under review. Twice has no
	// such trace, so its claim stands.
	config([]map[string]any{listRule, anyRule}, nil)
	r, errOut := run(t)
	list, twice := find(r, listWF), find(r, twiceWF)
	if len(list.Unexplained) != 1 || list.Unexplained[0].Acknowledged {
		t.Fatalf("unexplained: %+v\n%s", list.Unexplained, errOut)
	}
	if list.Classification.Buckets.CannotBeTested.Paths != 0 || list.Classification.Buckets.NeedsReview.Paths == 0 {
		t.Errorf("claims should be under review: %+v", list.Classification.Buckets)
	}
	if twice.Classification.Buckets.CannotBeTested.Paths == 0 || len(twice.Unexplained) != 0 {
		t.Errorf("twice must keep its claim: %+v", twice)
	}
	if !strings.Contains(errOut, "that fits no path") || !strings.Contains(errOut, list.Unexplained[0].StepsID) {
		t.Errorf("the warning must name the trace:\n%s", errOut)
	}
	reviewed := list.Classification.Buckets.NeedsReview.Paths

	// 3. Accepted with a reason: the claims come back, and the report still
	// shows the trace, as acknowledged, with the reason.
	config([]map[string]any{listRule, anyRule}, []map[string]any{{"workflow": listWF, "steps_id": list.Unexplained[0].StepsID, "reason": "helper returns early"}})
	r, errOut = run(t)
	list = find(r, listWF)
	if list.Classification.Buckets.CannotBeTested.Paths != reviewed || list.Classification.Buckets.NeedsReview.Paths != 0 ||
		len(list.Unexplained) != 1 || !list.Unexplained[0].Acknowledged || list.Unexplained[0].Reason != "helper returns early" {
		t.Errorf("acknowledged: %+v", list)
	}
	if strings.Contains(errOut, "that fits no path") && strings.Contains(errOut, "not trusted") {
		t.Errorf("no warning expected for an acknowledged trace:\n%s", errOut)
	}

	// 4. An acknowledgement needs a reason; a wrong id is a stale warning.
	config([]map[string]any{listRule}, []map[string]any{{"workflow": listWF, "steps_id": "t1:0123456789ab", "reason": ""}})
	if _, errOut, code = pathkit(t, "report", pkg, "--traces", traces, "--allow-stale", "--json"); code != 1 || !strings.Contains(errOut, `needs a "reason"`) {
		t.Errorf("no reason: exit %d\n%s", code, errOut)
	}
	config([]map[string]any{listRule}, []map[string]any{{"workflow": listWF, "steps_id": "t1:0123456789ab", "reason": "old"}})
	_, errOut = run(t)
	if !strings.Contains(errOut, "stale entry: explained_traces") {
		t.Errorf("stale acknowledgement not reported:\n%s", errOut)
	}
}
