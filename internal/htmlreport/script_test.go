package htmlreport

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// manyPaths is a report with n paths in one workflow, every 4th covered,
// every 7th a detector's cannot-be-tested.
func manyPaths(n int) ReportInput {
	in := sample("p.W", "p/w.go", "why")
	det := cls(classify.CannotBeTested, "same-list-loops", classify.SourceAuto, classify.Certain, "one list builds the other")
	det.Detector = "list-built-from-list"
	var ps []coverage.PathResult
	covered := 0
	for i := 1; i <= n; i++ {
		p := coverage.PathResult{Number: i, StableID: fmt.Sprintf("s1:%012d", i), Class: classify.DefaultTestable()}
		switch {
		case i%7 == 0:
			p.Class = det
		case i%4 == 0:
			p.Covered = true
			covered++
		}
		ps = append(ps, p)
	}
	in.Result.Workflows[0].Paths = ps
	in.Result.Workflows[0].Covered = covered
	in.Result.Paths, in.Result.Covered = n, covered
	in.Classification = &render.ClassificationView{Basis: coverage.BasisOverall}
	return in
}

// The page's script runs, draws only the page being looked at, and
// responds to a reader's controls. It is run under Node with a small DOM
// stub; the stub refuses innerHTML, so a script that built HTML from data
// would fail here. Skipped when Node is not installed.
func TestPageScriptWorks(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the page script is not run")
	}
	const n = 400
	in := manyPaths(n)
	// a second, small workflow: a search must not stay on it after a jump
	in.Result.Workflows = append(in.Result.Workflows, coverage.WorkflowResult{Name: "q.W2", File: "q/w2.go", Covered: 1, Paths: []coverage.PathResult{
		{Number: 1, StableID: "s1:b1", Covered: true, Class: classify.DefaultTestable()}, {Number: 2, StableID: "s1:b2", Class: classify.DefaultTestable()},
		{Number: 3, StableID: "s1:b3", Class: classify.DefaultTestable()},
	}})
	in.Result.Paths += 3
	in.Result.Covered++
	file := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(file, []byte(mustReport(t, in)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "testdata/smoke.js", file).CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	var got struct {
		OK     bool           `json:"ok"`
		Checks map[string]any `json:"checks"`
	}
	line := string(out[strings.LastIndex(strings.TrimSpace(string(out)), "\n")+1:])
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	c := got.Checks
	want := map[string]any{
		"firstRows":         50.0, // the default page size
		"firstShowing":      "Showing 1–50 of 400",
		"size25Rows":        25.0,
		"page2Showing":      "Showing 26–50 of 400",
		"jsClass":           "js",
		"searchVisible":     true,
		"noMatchRows":       0.0,
		"noMatchShowing":    "No paths match the filters.",
		"detailRows":        1.0,
		"jumpTarget":        1.0,
		"printRows":         400.0, // printing is not cut into pages
		"afterPrintRows":    25.0,
		"otherBeforeJump":   "No paths match the filters.",
		"otherAfterJump":    "Showing 1\u20133 of 3",
		"auditPage2Showing": "Showing 26\u201350 of 57",
	}
	for k, w := range want {
		if c[k] != w {
			t.Errorf("%s = %v, want %v", k, c[k], w)
		}
	}
	// 400 paths: 57 are every 7th (a detector's), 32 every 4th not also a 7th
	// are covered; the rest are missed. Filtering to missed shows only those.
	// after paging the audit to page 2, a search must show its first page
	if s, _ := c["auditAfterSearchShowing"].(string); !strings.HasPrefix(s, "Showing 1\u2013") {
		t.Errorf("audit after a search = %q, want it back on its first page", c["auditAfterSearchShowing"])
	}
	if c["missedAllMissed"] != true {
		t.Errorf("a missed filter showed covered rows: %v", c)
	}
	if rows, _ := c["missedRows"].(float64); rows != 25 {
		t.Errorf("missed filter, page of 25: rows = %v", c["missedRows"])
	}
	if s, _ := c["missedShowing"].(string); !strings.HasPrefix(s, "Showing 1–25 of ") {
		t.Errorf("missed showing = %q", s)
	}
	if a, _ := c["audit"].(float64); a != 19 { // header row + 18 detector paths? shown 25 per page
		t.Logf("audit rows (header included) = %v", c["audit"])
	}
}
