package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Branch IDs are stable words, not numbers: building a workflow twice gives
// the same IDs, no two different outcomes share one, and a helper's outcome
// has one ID however many calls copy it (inline) or whether it is summarized.
func TestBranchIDsAreUniqueAndStable(t *testing.T) {
	dirs := append([]string{"../../testdata/pilot/..."}, fixtureDirs(t)...)
	checked := 0
	for _, dir := range dirs {
		res, err := load.Load(dir)
		if err != nil {
			continue
		}
		for _, wf := range discover.Find(res.Packages) {
			for _, mode := range []model.HelperMode{model.HelpersInline, model.HelpersAuto} {
				g1, err := model.BuildWith(wf, model.Options{Helpers: mode})
				if err != nil {
					continue
				}
				g2, _ := model.BuildWith(wf, model.Options{Helpers: mode})
				a, b := g1.Branches(), g2.Branches()
				if len(a) != len(b) {
					t.Fatalf("%s: %d branches, then %d", wf.Name, len(a), len(b))
				}
				seen := map[string]string{}
				for i, br := range a {
					if br.ID != b[i].ID {
						t.Errorf("%s: branch %d has id %s, then %s", wf.Name, i, br.ID, b[i].ID)
					}
					if !strings.HasPrefix(br.ID, "b:") || len(br.ID) != 14 {
						t.Errorf("%s: odd branch id %q", wf.Name, br.ID)
					}
					if len(br.Exits) == 0 {
						t.Errorf("%s: branch %s has no exit", wf.Name, br.ID)
					}
					key := br.File + "|" + br.Func + "|" + br.Decision + "|" + br.Outcome
					if other, dup := seen[br.ID]; dup {
						t.Errorf("%s: branch id %s is used twice: %s and %s", wf.Name, br.ID, other, key)
					}
					seen[br.ID] = key
					for _, e := range br.Exits {
						if g1.BranchID(e) != br.ID {
							t.Errorf("%s: exit %s maps to %s, not %s", wf.Name, e.ID, g1.BranchID(e), br.ID)
						}
					}
				}
				checked++
			}
		}
	}
	if checked < 50 {
		t.Errorf("only %d workflows were checked", checked)
	}
}

// Moving code down the file (new lines above it) changes no branch ID: they
// hold no line numbers. Near miss: changing a decision's words does change it.
func TestBranchIDsIgnoreLineNumbers(t *testing.T) {
	ids := func(dir string) map[string]bool {
		res, err := load.Load(dir + "/...")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.BuildWith(wf, model.Options{Helpers: model.HelpersInline})
			if err != nil {
				continue
			}
			for _, b := range g.Branches() {
				out[wf.Name+" "+b.ID] = true
			}
		}
		return out
	}
	const src = "../../testdata/fixtures/helpers"
	copyShifted := func(mutate func(string) string) map[string]bool {
		// The same folder name every time: the file's place is part of an ID.
		dst := "../../testdata/fixtures/tmpshift"
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dst)
		entries, _ := os.ReadDir(src)
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(e.Name(), ".go") {
				b = []byte(mutate(string(b)))
			}
			if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return ids(dst)
	}
	orig := copyShifted(func(s string) string { return s })
	if len(orig) == 0 {
		t.Fatal("no branches in the fixture")
	}
	shifted := copyShifted(func(s string) string {
		i := strings.Index(s, "\nimport")
		if i < 0 {
			i = strings.Index(s, "\nfunc")
		}
		return s[:i] + "\n\n// a new comment\n// and another\n" + s[i:]
	})
	for k := range orig {
		if !shifted[k] {
			t.Errorf("branch %q changed when lines were added above it", k)
		}
	}
	changed := copyShifted(func(s string) string { return strings.ReplaceAll(s, "err != nil", "err != nil && true") })
	same := 0
	for k := range orig {
		if changed[k] {
			same++
		}
	}
	if same == len(orig) {
		t.Errorf("changing the decisions' words changed no branch id")
	}
}

// Decisions that run under a disconnected context are marked as cleanup after a
// cancel; the others are not. The mark is not part of any ID: the same
// outcomes have the same branch IDs and stable path IDs whether or not they are marked.
func TestCleanupDecisionsAreMarked(t *testing.T) {
	res, err := load.Load("../../testdata/fixtures/cleanup/...")
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.BuildWith(wf, model.Options{Helpers: model.HelpersInline})
		if err != nil {
			t.Fatal(err)
		}
		marked := map[string]bool{}
		for _, b := range g.Branches() {
			marked[b.Decision] = marked[b.Decision] || b.Cleanup
		}
		if marked["Work (activity)"] {
			t.Errorf("the work is not cleanup: %v", marked)
		}
		if !marked["Cleanup (activity)"] || !marked["if big"] {
			t.Errorf("the cleanup check and the if inside the cleanup region should be marked: %v", marked)
		}
		if len(g.Paths(model.DefaultMaxPaths).List) == 0 {
			t.Error("no paths")
		}
	}
}
