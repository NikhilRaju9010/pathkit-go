package baseline_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/baseline"
	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

func path(n int, id string, b classify.Bucket, covered bool) coverage.PathResult {
	return coverage.PathResult{Number: n, StableID: id, Covered: covered, Class: classify.Classification{Bucket: b}}
}

func result(ws ...coverage.WorkflowResult) coverage.Result { return coverage.Result{Workflows: ws} }

const (
	idA = "s1:aaaaaaaaaaaa"
	idB = "s1:bbbbbbbbbbbb"
	idC = "s1:cccccccccccc"
	idD = "s1:dddddddddddd"
	idE = "s1:eeeeeeeeeeee"
)

func ids(fs []baseline.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Workflow+" "+f.StableID+" was="+string(f.WasBucket))
	}
	return out
}

func TestCheck(t *testing.T) {
	before := baseline.From(result(coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{
		path(1, idA, classify.Testable, false),       // a known gap
		path(2, idB, classify.CannotBeTested, false), // will become testable
		path(3, idC, classify.NeedsReview, false),    // a known needs-review path
		path(4, idD, classify.Testable, true),        // will lose its test
	}}))

	now := result(
		coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{
			path(1, idA, classify.Testable, false),                  // known gap: not new
			path(2, idB, classify.Testable, false),                  // became testable, no test: new
			path(3, idC, classify.NeedsReview, false),               // still needs-review: not new
			path(4, idD, classify.Testable, false),                  // lost its test: not this check (floor)
			path(5, idE, classify.Testable, false),                  // a new path, no test: new
			path(6, "s1:ffffffffffff", classify.NeedsReview, false), // new needs-review
			path(7, "s1:111111111111", classify.Testable, true),     // new and tested: fine
		}},
		coverage.WorkflowResult{Name: "o.New", Paths: []coverage.PathResult{ // not in the baseline at all
			path(1, idA, classify.Testable, false),
			path(2, idB, classify.NeedsReview, false),
			path(3, idC, classify.CannotBeTested, false),
		}},
	)
	untested, review, lost := baseline.Check(before, now)
	if got, want := ids(untested), []string{"o.W " + idB + " was=cannot-be-tested", "o.W " + idE + " was=", "o.New " + idA + " was="}; !reflect.DeepEqual(got, want) {
		t.Errorf("untested = %q\nwant %q", got, want)
	}
	if got, want := ids(review), []string{"o.W s1:ffffffffffff was=", "o.New " + idB + " was="}; !reflect.DeepEqual(got, want) {
		t.Errorf("review = %q\nwant %q", got, want)
	}

	// Path 4 had a test in the baseline and has none now.
	if got, want := ids(lost), []string{"o.W " + idD + " was=testable"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lost = %q\nwant %q", got, want)
	}

	// A path that was testable and becomes needs-review is new to review.
	_, review, _ = baseline.Check(before, result(coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{path(1, idA, classify.NeedsReview, false)}}))
	if got, want := ids(review), []string{"o.W " + idA + " was=testable"}; !reflect.DeepEqual(got, want) {
		t.Errorf("review = %q, want %q", got, want)
	}

	// The same run as the baseline: nothing new.
	again := result(coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{
		path(4, idD, classify.Testable, true), path(1, idA, classify.Testable, false),
		path(2, idB, classify.CannotBeTested, false), path(3, idC, classify.NeedsReview, false),
	}})
	if u, r, l := baseline.Check(before, again); len(u)+len(r)+len(l) != 0 {
		t.Errorf("a run equal to its baseline found %q, %q and %q", ids(u), ids(r), ids(l))
	}
	// A covered path that becomes cannot-be-tested and loses its test is
	// still lost; one that disappears (its id is gone) is not.
	_, _, lost = baseline.Check(before, result(coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{path(4, idD, classify.CannotBeTested, false)}}))
	if len(lost) != 1 {
		t.Errorf("reclassified and lost: %q", ids(lost))
	}
	if _, _, lost = baseline.Check(before, result(coverage.WorkflowResult{Name: "o.W", Paths: []coverage.PathResult{}})); len(lost) != 0 {
		t.Errorf("a path that no longer exists is not lost: %q", ids(lost))
	}
}

func TestFromIsSortedAndRoundTrips(t *testing.T) {
	f := baseline.From(result(
		coverage.WorkflowResult{Name: "z.W", Truncated: true, Paths: []coverage.PathResult{path(2, idB, classify.Testable, true), path(1, idA, classify.CannotBeTested, false)}},
		coverage.WorkflowResult{Name: "a.W", Paths: []coverage.PathResult{}},
	))
	if f.Workflows[0].Name != "a.W" || f.Workflows[1].Paths[0].StableID != idA || !f.Workflows[1].Truncated {
		t.Fatalf("not sorted: %+v", f)
	}
	p := filepath.Join(t.TempDir(), "b.json")
	if err := baseline.Write(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := baseline.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f) {
		t.Errorf("round trip:\n%+v\nwant\n%+v", got, f)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), `"paths": []`) {
		t.Errorf("an empty workflow must be written as an empty list, not null:\n%s", raw)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct{ name, content, want string }{
		{"missing", "", "not found; write it with --write-baseline"},
		{"garbage", "{not json", "is not a PathKit baseline"},
		{"unknown key", `{"schemaVersion":1,"tool":"pathkit-go","note":"","workflows":[],"extra":1}`, `unknown field "extra"`},
		{"other tool", `{"schemaVersion":1,"tool":"x","note":"","workflows":[]}`, `"tool" is "x"`},
		{"other version", `{"schemaVersion":2,"tool":"pathkit-go","note":"","workflows":[]}`, "schemaVersion 2, but this PathKit reads only 1"},
	} {
		p := filepath.Join(dir, "none.json")
		if c.content != "" {
			p = write(c.name+".json", c.content)
		}
		_, err := baseline.Read(p)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
}

// With three or more identical decisions in a row,
// inserting or removing one shifts every number together, so the
// neighbourhood looks unchanged; the count of decisions with the text is
// what changes. Files written before the count existed (0) skip it.
func TestAnchorChangesCount(t *testing.T) {
	a := baseline.RuleAnchor{Rule: "r", Workflow: "w", Entry: "if x #2 --false", Before: []string{"if x --false", "if x --true"}, After: []string{"--true--> if x #3"}, Count: 3}
	b := a
	b.Count = 4
	if got := baseline.AnchorChanges([]baseline.RuleAnchor{a}, []baseline.RuleAnchor{b}); len(got) != 1 || !strings.Contains(got[0], "the workflow had 3 decisions with this text, now 4") {
		t.Errorf("count change: %q", got)
	}
	if got := baseline.AnchorChanges([]baseline.RuleAnchor{a}, []baseline.RuleAnchor{a}); len(got) != 0 {
		t.Errorf("no change: %q", got)
	}
	old := a
	old.Count = 0
	if got := baseline.AnchorChanges([]baseline.RuleAnchor{old}, []baseline.RuleAnchor{b}); len(got) != 0 {
		t.Errorf("a file without counts must not warn on the count: %q", got)
	}
}

// A baseline saved by a Windows editor can start with a UTF-8 byte order
// mark; it must read like the same file without one. A UTF-16 file cannot be
// read and says how to fix it. Two stray bytes stay a plain "not a baseline"
// error.
func TestReadByteOrderMark(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.json")
	f := baseline.From(result(coverage.WorkflowResult{Name: "a.W", Paths: []coverage.PathResult{}}))
	if err := baseline.Write(plain, f); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	put := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	got, err := baseline.Read(put("bom.json", append([]byte("\xef\xbb\xbf"), body...)))
	if err != nil {
		t.Fatalf("a file with a UTF-8 byte order mark: %v", err)
	}
	if !reflect.DeepEqual(got, f) {
		t.Errorf("read with a mark:\n%+v\nwant\n%+v", got, f)
	}

	for name, data := range map[string]string{"le.json": "\xff\xfe{\x00}\x00", "be.json": "\xfe\xff\x00{\x00}"} {
		p := put(name, []byte(data))
		_, err := baseline.Read(p)
		if err == nil || !strings.Contains(err.Error(), "UTF-16") || !strings.HasPrefix(err.Error(), "baseline file "+p) {
			t.Errorf("%s: err = %v, want a message that names UTF-16 and starts with the file", name, err)
		}
	}

	_, err = baseline.Read(put("two.json", append([]byte("\xef\xbb"), body...)))
	if err == nil || !strings.Contains(err.Error(), "is not a PathKit baseline") || strings.Contains(err.Error(), "UTF-16") {
		t.Errorf("two stray bytes should stay a plain not-a-baseline error, got %v", err)
	}
}

func TestCheckMainDropsAndTolerance(t *testing.T) {
	was := baseline.File{Workflows: []baseline.Workflow{{Name: "w.A", Main: &baseline.Main{Kind: "path", Covered: 4, Total: 5, Percent: 80}}, {Name: "w.B", Main: &baseline.Main{Kind: "scenario", Covered: 2, Total: 2, Percent: 100}}, {Name: "w.C"}}}
	res := coverage.Result{Workflows: []coverage.WorkflowResult{
		{Name: "w.A", Main: coverage.MainNumber{Kind: "path", Covered: 3, Total: 5, Percent: 60, Measured: true}},
		{Name: "w.B", Main: coverage.MainNumber{Kind: "branch", Covered: 1, Total: 4, Percent: 25, Measured: true}}, // another kind: not compared
		{Name: "w.C", Main: coverage.MainNumber{Kind: "path", Covered: 0, Total: 1, Percent: 0, Measured: true}},    // no main number in the baseline
	}}
	drops, changed := baseline.CheckMain(was, res, 0)
	if len(drops) != 1 || drops[0].Workflow != "w.A" || len(changed) != 1 {
		t.Fatalf("drops %+v changed %v", drops, changed)
	}
	if drops, _ := baseline.CheckMain(was, res, 25); len(drops) != 0 {
		t.Errorf("a drop of 20 points is within a tolerance of 25: %+v", drops)
	}
	res.Workflows[0].Main.Measured = false
	if drops, _ := baseline.CheckMain(was, res, 50); len(drops) != 1 || !drops[0].NotMeasured {
		t.Errorf("a main number that is gone is a drop whatever the tolerance: %+v", drops)
	}
}
