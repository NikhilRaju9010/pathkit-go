package model_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

const helperFixtures = "../../testdata/fixtures/helpers"

func helperGraph(t *testing.T, name string) *model.Graph {
	t.Helper()
	wf, ok := workflowsIn(t, helperFixtures)["helpers."+name]
	if !ok {
		t.Fatalf("fixture workflow helpers.%s not found", name)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return g
}

// Helpers that call the Temporal SDK are followed: one copy per call, its
// decisions are junctions, and the error check after the call has only the
// exit the helper's return allows.
func TestHelpersAreFollowed(t *testing.T) {
	tests := []struct {
		workflow string
		want     []string
	}{
		// charge fails when its activity fails (J1); the caller's check (J2)
		// can only go the way the helper returned.
		{"OneHelper", []string{"J1.failure J2.failure|failed", "J1.success J2.success|"}},
		// two calls, two copies: J1/J2 are the first call, J3/J4 the second.
		{"TwoCalls", []string{
			"J2.failure J1.failure|failed",
			"J2.success J1.success J4.failure J3.failure|failed",
			"J2.success J1.success J4.success J3.success|completed",
		}},
		// the helper's own branch and the activity error it returns.
		{"BranchHelper", []string{
			"J2.true J3.failure J1.failure|failed",
			"J2.true J3.success J1.failure|failed",
			"J2.true J3.success J1.success|completed",
			"J2.false J1.failure|failed",
			"J2.false J1.success|completed",
		}},
		// three helpers deep.
		{"ThreeDeep", []string{"J2.failure J1.failure|failed", "J2.success J1.success|completed"}},
		// a method of a known type, and a function literal set once.
		{"MethodHelper", []string{"J2.true J1.failure|failed", "J2.false J1.failure|failed", "J2.false J1.success|completed"}},
		{"ClosureHelper", []string{"J1.failure|failed", "J1.success|completed"}},
		// a loop inside a helper is a loop of the workflow; a helper called
		// from a loop makes the loop a decision.
		{"LoopHelper", []string{"J1.iterate J2.failure|failed", "J1.iterate J2.success J1.retry J1.exit|completed", "J1.exit|completed"}},
		{"InLoop", []string{
			"J1.iterate J3.failure J2.failure|failed",
			"J1.iterate J3.success J2.success J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.workflow, func(t *testing.T) {
			g := helperGraph(t, tc.workflow)
			if got := pathKeys(g); !slices.Equal(got, tc.want) {
				t.Errorf("paths:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

// What is not a helper that does workflow work, or cannot be followed safely,
// is left as it was: no copy of its code, and (for an error check on an
// ordinary call) the assumption that the check passes.
func TestHelperNearMisses(t *testing.T) {
	tests := []struct {
		workflow string
		sites    int
		want     []string
	}{
		// validate has decisions but never calls the SDK: ordinary code.
		{"PureHelper", 0, []string{"|"}},
		// calm never returns an error: the check after it is left alone.
		{"NeverFails", 1, []string{"|completed"}},
		// an interface method and a function value are opaque.
		{"OpaqueCalls", 0, []string{"|completed"}},
		// a call inside an expression is not followed; the if is an ordinary one.
		{"InsideExpression", 0, []string{"J1.true|", "J1.false|completed"}},
		// the helper's errors are a function value's: unknown, not "none".
		{"UnknownError", 1, []string{"J1.failure|failed", "J1.success|completed"}},
		// four helpers deep is one too many: the fourth is not followed, so
		// its branch is not a junction. `one` and `two` and `three` are.
		{"FourDeep", 3, []string{"J2.true J1.failure|failed", "J2.true J1.success|completed", "J2.false J1.success|completed"}},
		// an error check the walk only assumes passes (a call that is not
		// Temporal's), and a deferred function that can clear a named result:
		// what the helper returns is not certain, so both exits stay.
		{"AssumedCheck", 1, []string{"J1.failure|completed", "J1.success|"}},
		{"DeferredNil", 1, []string{"J1.failure|completed", "J1.success|"}},
		// a helper that calls itself is followed once, the recursion is not.
		{"Recursive", 1, []string{
			"J2.true J1.success|completed",
			"J2.false J3.failure J1.failure|failed",
			"J2.false J3.success J1.failure|failed",
			"J2.false J3.success J1.success|completed",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.workflow, func(t *testing.T) {
			g := helperGraph(t, tc.workflow)
			if got := len(g.Sites()); got != tc.sites {
				t.Errorf("%d copies of helper code, want %d", got, tc.sites)
			}
			if got := pathKeys(g); !slices.Equal(got, tc.want) {
				t.Errorf("paths:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

// The same helper called twice gives two sets of junctions with different
// stable texts, and no two paths share a stable id.
func TestHelperCopiesHaveTheirOwnStableIDs(t *testing.T) {
	g := helperGraph(t, "TwoCalls")
	var inner []string
	for _, j := range g.Junctions {
		if j.Site != nil {
			inner = append(inner, j.StableText())
		}
	}
	if len(inner) != 2 || inner[0] == inner[1] {
		t.Fatalf("stable texts of the two copies: %q", inner)
	}
	for _, want := range []string{"charge", "charge(ctx, 1)", "charge(ctx, 2)"} {
		if !strings.Contains(strings.Join(inner, "|"), want) {
			t.Errorf("stable texts %q do not name %q", inner, want)
		}
	}
	seen := map[string]bool{}
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if seen[p.StableID()] {
			t.Errorf("stable id %s is used twice", p.StableID())
		}
		seen[p.StableID()] = true
	}
}

// An id saved before helpers were followed is found again among the paths
// that took its place.
func TestLegacyStableIDs(t *testing.T) {
	// Before: the helper call and the check after it were not decisions, so
	// OneHelper had one path, from Start to an unknown end ("End").
	sum := sha256.Sum256([]byte(model.StableIDVersion + "\n" + "|"))
	old := model.StableIDVersion + ":" + hex.EncodeToString(sum[:])[:12]
	g := helperGraph(t, "OneHelper")
	found := 0
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if slices.Contains(p.LegacyStableIDs(), old) {
			found++
		}
		if p.StableID() == old {
			t.Errorf("the new id %s is the old one", old)
		}
	}
	if found != 2 {
		t.Errorf("%d paths list the old id %s, want both paths of OneHelper", found, old)
	}
}

// A trace through a helper carries markers; the matcher takes them out,
// checks they fit each other and the map, and matches the rest.
func TestMatchWithHelperMarkers(t *testing.T) {
	g := helperGraph(t, "OneHelper")
	s := g.Sites()
	if len(s) != 1 {
		t.Fatalf("%d sites", len(s))
	}
	id := s[0].ID
	p, mm := g.Match([]string{">" + id, "J1.failure", "<" + id, "J2.failure"})
	if mm != nil {
		t.Fatalf("a good trace does not match: %v", mm)
	}
	if p.Key() != "J1.failure J2.failure|failed" {
		t.Errorf("matched %s", p.Key())
	}
	bad := map[string][]string{
		"a step in the wrong copy":     {"J1.failure", "J2.failure"},
		"entered a copy never left":    {">" + id, "J1.failure", "J2.failure"},
		"left a copy never entered":    {"J1.failure", "<" + id, "J2.failure"},
		"an unknown copy":              {">S9", "<S9"},
		"the check after the helper":   {">" + id, "J1.failure", "<" + id, "J2.success"},
		"a step of the helper outside": {">" + id, "<" + id, "J1.failure", "J2.failure"},
	}
	for name, steps := range bad {
		if _, mm := g.Match(steps); mm == nil {
			t.Errorf("%s: %v matched", name, steps)
		}
	}
}

// A workflow whose helpers give it far more paths than the path cap still
// follows them: the count is exact, and the listing is what is cut.
func TestHelpersAreFollowedPastThePathCap(t *testing.T) {
	g := helperGraph(t, "Explosion")
	if len(g.Sites()) != 11 {
		t.Errorf("%d copies of helper code, want 11 (one per call)", len(g.Sites()))
	}
	if got := g.PathCount().String(); got != "2048" {
		t.Errorf("PathCount = %s, want 2048 (2 to the 11th)", got)
	}
	if ps := g.Paths(model.DefaultMaxPaths); !ps.Truncated || len(ps.List) != model.DefaultMaxPaths {
		t.Errorf("%d paths listed (truncated %v), want the first %d", len(ps.List), ps.Truncated, model.DefaultMaxPaths)
	}
}
