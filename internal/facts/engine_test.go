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
	unknown  []string
	paths    []string
	proofs   map[string][]ProofStep
	off      string
	assumed  map[string][]string // path key -> assumptions the claim rests on
}

func analyzeFixture(t *testing.T, dir string) map[string]found {
	return analyzeFixtureWith(t, dir, Options{JSONDecode: true, ArgsCopied: true})
}

func analyzeFixtureWith(t *testing.T, dir string, opts Options) map[string]found {
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
		a := New(g, opts)
		f := found{claims: map[string]Kind{}, proofs: map[string][]ProofStep{}, assumed: map[string][]string{}, off: a.Off()}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			text := p.Key()
			f.paths = append(f.paths, text)
			r := a.Analyze(p)
			if r.Claim != nil {
				f.claims[text] = r.Claim.Kind
				f.proofs[text] = r.Claim.Proof
				f.assumed[text] = r.Claim.Assumptions
			}
			if r.Suspect != nil {
				f.suspects = append(f.suspects, text)
			}
			if r.Unknown != nil {
				f.unknown = append(f.unknown, text)
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
		for _, k := range f.unknown {
			fmt.Printf("    unknown %s\n", k)
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
			if fmt.Sprint(f.unknown) != fmt.Sprint(wantUnknown[name]) && !(len(f.unknown) == 0 && len(wantUnknown[name]) == 0) {
				t.Errorf("unknown-input candidates %v, want %v", f.unknown, wantUnknown[name])
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

// With the default-json-converter assumption off, nothing is claimed about
// decoded values: the fixtures that rest on it make no claim.
func TestJSONRuleOff(t *testing.T) {
	all := analyzeFixtureWith(t, engineDir, Options{JSONDecode: false, ArgsCopied: true})
	for _, name := range []string{"ImpossibleJSONParam", "ImpossibleJSONMapParam", "ImpossibleJSONNested", "ImpossibleJSONGet"} {
		if f := all[name]; len(f.claims) != 0 {
			t.Errorf("%s: claims with the rule off: %v", name, f.claims)
		}
	}
	// ... and on, they do (and say what they rest on).
	on := analyzeFixture(t, engineDir)
	if len(on["ImpossibleJSONParam"].claims) == 0 {
		t.Error("the rule on makes no claim on ImpossibleJSONParam")
	}
	for key, proof := range on["ImpossibleJSONParam"].proofs {
		text := ""
		for _, p := range proof {
			text += p.Text + "\n"
		}
		if !strings.Contains(text, "default JSON converter") {
			t.Errorf("%s: the proof does not name the assumption:\n%s", key, text)
		}
	}
}

// The proof of a type switch claim (shape b): the stores, the decision, and
// what it adds up to.
func TestProofTextForTheTypeSwitchShape(t *testing.T) {
	f := analyzeFixture(t, "../../testdata/shapes/typeswitch")["typeswitch.SwitchOnLocalMap"]
	var got []string
	for _, s := range f.proofs["J1.case int|completed"] {
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
		"`vals` is built here. [line 18]",
		"`vals` receives a value in `vals[\"name\"]`. [line 19]",
		"`vals` receives a value in `vals[\"alias\"]`. [line 20]",
		"This is a `string`. [line 19]",
		"This is a `*string`. [line 20]",
		"`vals[key]` may not have that key, and a missing key gives nil.",
		"J1 (switch vals[key].(type)) takes `case int`. [step 1, line 21]",
		"`vals[key]` can hold `string`, `*string`, nil, none of which takes `case int`, so that case cannot be taken.",
	}
	if len(got) < 4 || !strings.Contains(strings.Join(got, "\n"), "takes `case int`. [step 1,") || !strings.Contains(strings.Join(got, "\n"), "`string`, `*string`") {
		t.Errorf("proof:\n%s\n(for reference, the shape of a good proof:\n%s)", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// What encoding/json decodes in the workflow follows from the standard
// library, not from an assumption about Temporal: the claims stay with the
// default-json-converter rule off, and name no assumption. A value Temporal
// decoded needs the rule.
func TestStdJSONNeedsNoAssumption(t *testing.T) {
	off := analyzeFixtureWith(t, engineDir, Options{JSONDecode: false, ArgsCopied: true})
	on := analyzeFixtureWith(t, engineDir, Options{JSONDecode: true, ArgsCopied: true})
	for _, name := range []string{"ImpossibleStdUnmarshal", "ImpossibleStdDecoder", "ImpossibleStdUnmarshalDespiteOtherUseNumber"} {
		for _, f := range []found{off[name], on[name]} {
			if len(f.claims) == 0 {
				t.Errorf("%s: no claim (rule on or off)", name)
			}
			for key, as := range f.assumed {
				if len(as) != 0 {
					t.Errorf("%s %s: rests on %v, want no assumption", name, key, as)
				}
			}
		}
	}
	if n := len(off["ImpossibleJSONGet"].claims); n != 0 {
		t.Errorf("ImpossibleJSONGet: %d claims with the rule off, want 0", n)
	}
	if n := len(on["ImpossibleJSONGet"].claims); n == 0 {
		t.Error("ImpossibleJSONGet: no claim with the rule on")
	}
}

// A map handed to a call that encodes its arguments keeps its facts, and the
// claim names the assumption no-custom-interceptor; with that assumption off
// (the module registers a custom interceptor) the same code makes no claim.
func TestArgsCopiedNeedsTheInterceptorAssumption(t *testing.T) {
	on := analyzeFixtureWith(t, engineDir, Options{JSONDecode: true, ArgsCopied: true})
	off := analyzeFixtureWith(t, engineDir, Options{JSONDecode: true, ArgsCopied: false})
	for _, name := range []string{"ImpossibleMapPassedToActivity", "ImpossibleMapInsideActivityInput", "ImpossibleMapPassedToChildWorkflow", "ImpossibleMapSignalled"} {
		if len(on[name].claims) == 0 {
			t.Errorf("%s: no claim with the assumption on", name)
		}
		for key, as := range on[name].assumed {
			if len(as) != 1 || as[0] != AssumptionInterceptor {
				t.Errorf("%s %s: rests on %v, want [%s]", name, key, as, AssumptionInterceptor)
			}
		}
		if n := len(off[name].claims); n != 0 {
			t.Errorf("%s: %d claims with the assumption off, want 0", name, n)
		}
	}
	for _, name := range []string{"PossibleMapPassedToLocalActivity", "PossibleMapInsideKeptInput", "PossibleMapPassedToActivityAndCall"} {
		if n := len(on[name].claims); n != 0 {
			t.Errorf("%s: %d claims, want 0 (the relaxation is only for arguments encoded at the call)", name, n)
		}
	}
}
