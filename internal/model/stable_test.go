package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func buildStable(t *testing.T, name string) *model.Graph {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/fixtures/stable")["stable."+name]
	if !ok {
		t.Fatalf("fixture workflow stable.%s not found", name)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// byStableKey maps each path's stable key to its path.
func byStableKey(g *model.Graph) map[string]model.Path {
	m := map[string]model.Path{}
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		m[p.StableKey()] = p
	}
	return m
}

// Adding a junction must not change the stableId of paths that do not go
// through it, while the older id (built from J-numbers) does change. That
// is the whole reason stableId exists.
func TestStableIDSurvivesAnAddedJunction(t *testing.T) {
	before, after := byStableKey(buildStable(t, "Before")), byStableKey(buildStable(t, "After"))

	// The unrelated paths: those where `if a` is false.
	unrelated := 0
	idChanged := 0
	for key, b := range before {
		if !strings.Contains(key, "false") || strings.HasPrefix(key, "if a\x1ftrue") {
			continue
		}
		a, ok := after[key]
		if !ok {
			t.Errorf("an unrelated path vanished after adding a junction: %q", key)
			continue
		}
		unrelated++
		if a.StableID() != b.StableID() {
			t.Errorf("stableId changed for an unrelated path: %s -> %s\n%q", b.StableID(), a.StableID(), key)
		}
		if a.ID() != b.ID() {
			idChanged++ // the old id moved because the junctions were renumbered
		}
	}
	if unrelated != 3 {
		t.Errorf("compared %d unrelated paths, want 3 (b true, c true, none)", unrelated)
	}
	if idChanged == 0 {
		t.Error("the old id never changed, so this test would not notice a stableId that is really just the old id")
	}

	// The path through the new junction is a different path: new stableId.
	for key, a := range after {
		if _, ok := before[key]; ok {
			continue
		}
		for _, b := range before {
			if a.StableID() == b.StableID() {
				t.Errorf("new path %q reuses the stableId of an old path", key)
			}
		}
	}

	// Removing the junction is the same comparison the other way round.
	for key, b := range before {
		if a, ok := after[key]; ok && a.StableID() != b.StableID() {
			t.Errorf("removing the junction would change %q", key)
		}
	}
}

func TestStableIDShape(t *testing.T) {
	g := buildStable(t, "After")
	seen := map[string]bool{}
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		id := p.StableID()
		if !strings.HasPrefix(id, "s1:") || len(id) != len("s1:")+12 {
			t.Errorf("stableId %q: want s1: and 12 hex digits", id)
		}
		if seen[id] {
			t.Errorf("stableId %s is used by two paths of one workflow", id)
		}
		seen[id] = true
		if strings.Contains(p.StableKey(), "J1") || strings.Contains(p.StableKey(), "J2") {
			t.Errorf("the stable key has a J-number: %q", p.StableKey())
		}
	}
}

// The same code gives the same stableIds on every call.
func TestStableIDIsRepeatable(t *testing.T) {
	ids := func() []string {
		var out []string
		for _, p := range buildStable(t, "Before").Paths(model.DefaultMaxPaths).List {
			out = append(out, p.StableID())
		}
		return out
	}
	if a, b := ids(), ids(); !slices.Equal(a, b) {
		t.Errorf("stableIds differ between two builds: %v vs %v", a, b)
	}
}

// Two junctions with exactly the same text are told apart with " #2".
func TestStableIDSameTextJunctions(t *testing.T) {
	g := buildStable(t, "SameText")
	paths := g.Paths(model.DefaultMaxPaths).List
	seen := map[string]bool{}
	sawSecond := false
	for _, p := range paths {
		if seen[p.StableID()] {
			t.Errorf("two paths share stableId %s", p.StableID())
		}
		seen[p.StableID()] = true
		if strings.Contains(p.StableKey(), "if a #2") {
			sawSecond = true
		}
	}
	if len(paths) != 4 || !sawSecond {
		t.Errorf("%d paths, second junction named #2: %v; want 4 and true", len(paths), sawSecond)
	}
}

// Reformatting a case list across lines must not change the stableId: the
// exit label is normalized like the junction text.
func TestStableIDIgnoresLineBreaksInCaseList(t *testing.T) {
	one := buildStable(t, "OneLineCase").Paths(model.DefaultMaxPaths).List
	multi := buildStable(t, "MultiLineCase").Paths(model.DefaultMaxPaths).List
	if len(one) != len(multi) || len(one) != 2 {
		t.Fatalf("%d and %d paths, want 2 and 2", len(one), len(multi))
	}
	for i := range one {
		if one[i].StableID() != multi[i].StableID() {
			t.Errorf("path %d: %s vs %s\n%q\n%q", i+1, one[i].StableID(), multi[i].StableID(), one[i].StableKey(), multi[i].StableKey())
		}
	}
}
