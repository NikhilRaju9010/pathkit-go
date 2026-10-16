package facts

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

const engineDir = "../../testdata/fixtures/factsengine"

// found is what the engine says about one workflow's paths.
type found struct {
	claims   map[string]Kind // path text -> kind
	suspects []string
	paths    []string
	proofs   map[string][]ProofStep
	off      string
}

func analyzeFixture(t *testing.T, dir string) map[string]found {
	t.Helper()
	res, err := load.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]found{}
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.Build(wf)
		if err != nil {
			t.Fatalf("%s: %v", wf.Name, err)
		}
		a := New(g)
		f := found{claims: map[string]Kind{}, proofs: map[string][]ProofStep{}, off: a.Off()}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			text := p.Key()
			f.paths = append(f.paths, text)
			r := a.Analyze(p)
			if r.Claim != nil {
				f.claims[text] = r.Claim.Kind
				f.proofs[text] = r.Claim.Proof
			}
			if r.Suspect != nil {
				f.suspects = append(f.suspects, text)
			}
		}
		out[strings.TrimPrefix(wf.Name, "factsengine.")] = f
	}
	return out
}

func TestDumpEngine(t *testing.T) {
	if os.Getenv("PATHKIT_FACTS_DUMP") == "" {
		t.Skip("set PATHKIT_FACTS_DUMP=1 to print what the engine says")
	}
	dir := engineDir
	if d := os.Getenv("PATHKIT_FACTS_DIR"); d != "" {
		dir = d
	}
	all := analyzeFixture(t, dir)
	var names []string
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := all[n]
		fmt.Printf("%s (paths %d, off %q)\n", n, len(f.paths), f.off)
		var keys []string
		for k := range f.claims {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    claim   %-40s %s\n", k, f.claims[k])
		}
		for _, k := range f.suspects {
			fmt.Printf("    suspect %s\n", k)
		}
	}
}

// The engine says exactly what is listed in expected_test.go, no more: every
// Possible... workflow stays possible on every path (the safety list), every
// Impossible... workflow has its impossible paths proved.
func TestEngineClaimsOnFixtures(t *testing.T) {
	all := analyzeFixture(t, engineDir)
	if len(all) < 40 {
		t.Fatalf("only %d fixture workflows found", len(all))
	}
	for name, f := range all {
		t.Run(name, func(t *testing.T) {
			want := wantClaims[name]
			for key, kind := range f.claims {
				if got, ok := want[key]; !ok {
					t.Errorf("unexpected claim %s: %s (a false \"impossible\"? check the code by hand)", key, kind)
				} else if got != kind {
					t.Errorf("claim %s: label %s, want %s", key, kind, got)
				}
			}
			for key, kind := range want {
				if _, ok := f.claims[key]; !ok {
					t.Errorf("missing claim %s: %s", key, kind)
				}
			}
			if strings.HasPrefix(name, "Possible") && len(f.claims) > 0 {
				t.Errorf("a Possible... workflow must make no claim, made %d", len(f.claims))
			}
			if strings.HasPrefix(name, "Impossible") && len(f.claims) == 0 {
				t.Errorf("an Impossible... workflow must make a claim")
			}
			if fmt.Sprint(f.suspects) != fmt.Sprint(wantSuspects[name]) && !(len(f.suspects) == 0 && len(wantSuspects[name]) == 0) {
				t.Errorf("suspects %v, want %v", f.suspects, wantSuspects[name])
			}
			if f.off != wantOff[name] {
				t.Errorf("engine off = %q, want %q", f.off, wantOff[name])
			}
		})
	}
}

// Every claim has a proof: the facts, the decision, and a closing sentence.
func TestEveryClaimHasAProof(t *testing.T) {
	for name, f := range analyzeFixture(t, engineDir) {
		for key, p := range f.proofs {
			if len(p) < 2 {
				t.Errorf("%s %s: proof has %d steps", name, key, len(p))
				continue
			}
			last := p[len(p)-1]
			if last.Step != 0 || last.Line != 0 || last.Text == "" {
				t.Errorf("%s %s: last step should be the closing sentence, got %+v", name, key, last)
			}
			positional := 0
			for _, s := range p {
				if s.Text == "" {
					t.Errorf("%s %s: empty proof sentence", name, key)
				}
				if s.Step > 0 {
					positional++
				}
			}
			if positional == 0 {
				t.Errorf("%s %s: no proof sentence names a step", name, key)
			}
		}
	}
}

// The proof of shape (a): what is written down, in order, in plain words.
func TestProofTextForTheValueFlowShape(t *testing.T) {
	all := analyzeFixture(t, "../../testdata/shapes/valueflow")
	f := all["valueflow.ContradictionThroughAssign"]
	// the file is valueflow.go; its lines are fixed by the fixture
	var got []string
	for _, s := range f.proofs["J1.false J2.false|completed"] {
		pos := ""
		switch {
		case s.Step > 0:
			pos = fmt.Sprintf(" [step %d, line %d]", s.Step, s.Line)
		case s.Line > 0:
			pos = fmt.Sprintf(" [line %d]", s.Line)
		}
		got = append(got, s.Text+pos)
	}
	want := []string{
		"`id` is set to \"\". [line 17]",
		"J1 (if ok) takes the false side, so `id = v` is skipped. [step 1, line 19]",
		"J2 (if id == \"\") takes the false side. [step 2, line 22]",
		"Nothing on the path changes this in between, so `id == \"\"` is always true at that point and the false side cannot be taken.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("proof:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestThreeValuedHelpers(t *testing.T) {
	if isTrue.not() != isFalse || isFalse.not() != isTrue || unknown.not() != unknown {
		t.Error("not() is wrong")
	}
}
