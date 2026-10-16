// Package shapes holds the test of the shape fixtures in testdata/shapes:
// small made-up workflows, one for each code shape PathKit v2 has to
// understand, with a hand-written expected answer for every path.
//
// Each fixture folder has an expected.json that lists, for every workflow:
//
//   - "today": what PathKit says now, path by path (checked to be exact);
//   - "v2": what v2 should say, written by hand;
//   - on every v2 entry, "status": "today: correct" when today's answer is
//     the v2 answer, "today: missed" when it is not (checked too).
//
// So the test passes now and keeps a list of the gaps v2 has to close. When
// v2 closes one, "today" and "status" change in the same commit, and a
// reviewer sees the change. The "v2" answers are never changed to make a
// test pass.
package shapes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const shapesDir = "../../testdata/shapes"

// answer is a bucket and a label ("" for a plain testable path).
type answer struct {
	Bucket string `json:"bucket"`
	Label  string `json:"label"`
}

type todayPath struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	answer
}

type v2Path struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	// Provisional: the path does not exist today (v2 splits or adds
	// steps), so Key is only a name for it, not a real path key.
	Provisional bool   `json:"provisional,omitempty"`
	V2          answer `json:"v2"`
	Status      string `json:"status"`
	Note        string `json:"note,omitempty"`
}

type workflowCase struct {
	Today []todayPath `json:"today"`
	V2    []v2Path    `json:"v2"`
}

type expectedFile struct {
	Shape       string                  `json:"shape"`
	Description string                  `json:"description"`
	Workflows   map[string]workflowCase `json:"workflows"`
}

func fixtureDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(shapesDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// todayResults runs PathKit as it is now on one fixture folder, with
// classification on and no config rules, and lists every path of every
// workflow.
func todayResults(t *testing.T, dir string) map[string][]todayPath {
	t.Helper()
	res, err := load.Load(filepath.Join(shapesDir, dir))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := classify.NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var wfs []coverage.Workflow
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.Build(wf)
		if err != nil {
			t.Fatalf("%s: %v", wf.Name, err)
		}
		wfs = append(wfs, coverage.Workflow{Name: wf.Name, Graph: g, Hash: model.FunctionHash(wf.Pkg.Fset, wf.Func)})
	}
	out := map[string][]todayPath{}
	for _, w := range coverage.Compute(coverage.Input{Workflows: wfs, Classifier: eng.Classify}).Workflows {
		for _, p := range w.Paths {
			out[w.Name] = append(out[w.Name], todayPath{Key: p.Path.Key(), Text: strings.TrimPrefix(render.PathText(p.Path), "Start -> "),
				answer: answer{string(p.Class.Bucket), p.Class.Label}})
		}
	}
	return out
}

func TestShapeFixtures(t *testing.T) {
	missed := 0
	for _, dir := range fixtureDirs(t) {
		t.Run(dir, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(shapesDir, dir, "expected.json"))
			if os.IsNotExist(err) && dir == "gotofn" {
				return // checked in TestGotoIsRefused
			}
			if err != nil {
				t.Fatal(err)
			}
			var want expectedFile
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			got := todayResults(t, dir)
			if len(got) == 0 {
				t.Fatal("PathKit found no workflow in this fixture")
			}
			if len(got) != len(want.Workflows) {
				t.Errorf("fixture has %d workflows, expected.json lists %d", len(got), len(want.Workflows))
			}
			for name, actual := range got {
				w, ok := want.Workflows[name]
				if !ok {
					t.Errorf("%s: not listed in expected.json", name)
					continue
				}
				// 1. "today" is exactly what PathKit says now.
				if fmt.Sprint(w.Today) != fmt.Sprint(actual) {
					t.Errorf("%s: PathKit's answer today changed.\n got  %v\n want %v\n(if v2 work closed a gap, update \"today\" and \"status\" together, and have the change reviewed)", name, actual, w.Today)
				}
				// 2. every v2 entry says correctly whether today matches it.
				todayByKey := map[string]todayPath{}
				for _, p := range actual {
					todayByKey[p.Key] = p
				}
				seen := map[string]bool{}
				for _, v := range w.V2 {
					seen[v.Key] = true
					wantStatus := "today: missed"
					if p, ok := todayByKey[v.Key]; ok && !v.Provisional && p.answer == v.V2 {
						wantStatus = "today: correct"
					}
					if v.Status != wantStatus {
						t.Errorf("%s %s: status %q, but it is %q", name, v.Key, v.Status, wantStatus)
					}
					if v.Status == "today: missed" {
						missed++
						t.Logf("GAP %s [%s] today %v, v2 should say %v", name, v.Text, todayByKey[v.Key].answer, v.V2)
					}
					if _, ok := todayByKey[v.Key]; !ok && !v.Provisional {
						t.Errorf("%s: v2 lists path %s that does not exist today (mark it \"provisional\")", name, v.Key)
					}
				}
				// 3. unless v2 changes the steps, it lists every path.
				hasProvisional := false
				for _, v := range w.V2 {
					hasProvisional = hasProvisional || v.Provisional
				}
				if !hasProvisional {
					for _, p := range actual {
						if !seen[p.Key] {
							t.Errorf("%s: path %s has no v2 answer", name, p.Key)
						}
					}
				}
			}
		})
	}
	t.Logf("%d path answers v2 has to change (\"today: missed\")", missed)
}

// A workflow that uses goto is refused with a clear message, never guessed.
func TestGotoIsRefused(t *testing.T) {
	res, err := load.Load(filepath.Join(shapesDir, "gotofn"))
	if err != nil {
		t.Fatal(err)
	}
	found := discover.Find(res.Packages)
	if len(found) == 0 {
		t.Fatal("no workflow found in the goto fixture")
	}
	for _, wf := range found {
		_, err := model.Build(wf)
		if err == nil || !strings.Contains(err.Error(), "goto") || !strings.Contains(err.Error(), "not supported") {
			t.Errorf("%s: want a clear \"goto ... not supported\" error, got %v", wf.Name, err)
		}
	}
}

// TestDumpToday prints what PathKit says today for every fixture, to help
// write a new expected.json: PATHKIT_SHAPES_DUMP=1 go test ./internal/shapes -run Dump -v
func TestDumpToday(t *testing.T) {
	if os.Getenv("PATHKIT_SHAPES_DUMP") == "" {
		t.Skip("set PATHKIT_SHAPES_DUMP=1 to print today's answers")
	}
	for _, dir := range fixtureDirs(t) {
		if dir == "gotofn" {
			continue
		}
		b, _ := json.MarshalIndent(todayResults(t, dir), "", "  ")
		fmt.Printf("=== %s\n%s\n", dir, b)
	}
}
