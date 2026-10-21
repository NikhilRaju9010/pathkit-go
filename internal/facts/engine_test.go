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
	defects  []string // paths that take a case only a dereferenced pointer could take
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
			if r.Defect != nil {
				f.defects = append(f.defects, text)
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
			if fmt.Sprint(f.defects) != fmt.Sprint(wantDefects[name]) && !(len(f.defects) == 0 && len(wantDefects[name]) == 0) {
				t.Errorf("likely defects %v, want %v", f.defects, wantDefects[name])
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

// A stage path the engine calls impossible in every incoming state makes every
// full path through it impossible when the paths are followed whole. Checked
// on the engine fixtures and the shape fixtures with several stage sizes and
// context limits (limit 1 cuts the states off, so claims become pending).
func TestStageClaimsAgreeWithWholePaths(t *testing.T) {
	dirs := []string{engineDir, "../../testdata/shapes/valueflow", "../../testdata/shapes/typeswitch", "../../testdata/shapes/zerovalue", "../../testdata/shapes/guardedtwice", "../../testdata/fixtures/bigflow"}
	checked, impossibleStages := 0, 0
	for _, dir := range dirs {
		res, err := load.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			if err != nil {
				continue
			}
			ps := g.Paths(5000)
			if ps.Truncated {
				continue
			}
			a := New(g, Options{JSONDecode: true, ArgsCopied: true})
			for _, maxPaths := range []int{2, 8, 256} {
				stages := g.Stages(maxPaths)
				for _, maxCtx := range []int{1, 16} {
					reps := a.AnalyzeStages(g, stages, maxCtx)
					imp := map[int]map[string]bool{}
					for k, rep := range reps {
						imp[k] = map[string]bool{}
						for _, r := range rep.Results {
							if r.Verdict == Impossible {
								imp[k][r.Path.Key()] = true
								impossibleStages++
							}
						}
					}
					for _, p := range ps.List {
						whole := a.Analyze(p).Claim != nil
						for _, pc := range g.StagePieces(p, stages) {
							if imp[pc.Stage][pc.Key()] && !whole {
								t.Errorf("%s (stages %d, contexts %d): stage %d path %s is called impossible, but the whole path %s has no claim", wf.Name, maxPaths, maxCtx, pc.Stage, pc.Key(), p.Key())
							}
						}
					}
					checked++
				}
			}
		}
	}
	if checked < 100 || impossibleStages == 0 {
		t.Errorf("checked %d runs, %d impossible stage paths", checked, impossibleStages)
	}
}

// Listing with pruning leaves out exactly the paths the engine calls
// impossible at a step, counts them, and lists the rest unchanged.
func TestPrunedListingAddsUp(t *testing.T) {
	dirs := []string{engineDir, "../../testdata/shapes/valueflow", "../../testdata/shapes/typeswitch", "../../testdata/shapes/zerovalue", "../../testdata/shapes/guardedtwice", "../../testdata/shapes/loopchange"}
	groups, checked := 0, 0
	for _, dir := range dirs {
		res, err := load.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			if err != nil {
				continue
			}
			all := g.Paths(1 << 20)
			if all.Truncated {
				continue
			}
			a := New(g, Options{JSONDecode: true, ArgsCopied: true})
			pl := g.PathsPruned(1<<20, func() any { return a.NewWalk() },
				func(s any) any { return s.(*Walk).Clone() },
				func(s any, st model.Step) any {
					if c := s.(*Walk).Step(st); c != nil {
						return c
					}
					return nil
				})
			listed := map[string]bool{}
			for _, p := range pl.List {
				listed[p.Key()] = true
				if a.Analyze(p).Claim != nil {
					t.Errorf("%s: listed path %s has a claim", wf.Name, p.Key())
				}
			}
			under := 0
			for _, p := range all.List {
				inGroup := 0
				for _, gr := range pl.Pruned {
					if hasPrefix(p, gr.Prefix) {
						inGroup++
					}
				}
				switch {
				case listed[p.Key()] && inGroup != 0:
					t.Errorf("%s: path %s is listed and pruned", wf.Name, p.Key())
				case !listed[p.Key()] && inGroup != 1:
					t.Errorf("%s: path %s is in %d pruned groups (want 1)", wf.Name, p.Key(), inGroup)
				}
				if !listed[p.Key()] {
					under++
					if a.Analyze(p).Claim == nil {
						t.Errorf("%s: pruned path %s has no claim when walked whole", wf.Name, p.Key())
					}
				}
			}
			sum := 0
			for _, gr := range pl.Pruned {
				sum += int(gr.Count.Int64())
			}
			if sum != under || len(pl.List)+under != len(all.List) {
				t.Errorf("%s: listed %d + pruned %d (groups say %d) != %d paths", wf.Name, len(pl.List), under, sum, len(all.List))
			}
			groups += len(pl.Pruned)
			checked++
		}
	}
	if checked < 40 || groups == 0 {
		t.Errorf("checked %d workflows, %d pruned groups", checked, groups)
	}
}

func hasPrefix(p model.Path, prefix []model.Step) bool {
	if len(p.Steps) < len(prefix) {
		return false
	}
	for i, s := range prefix {
		if p.Steps[i].Exit != s.Exit {
			return false
		}
	}
	return true
}

// bigflowWorkflow builds one workflow of the bigflow fixture.
func bigflowWorkflow(t *testing.T, name string) (*model.Graph, *Analyzer) {
	t.Helper()
	res, err := load.Load("../../testdata/fixtures/bigflow")
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range discover.Find(res.Packages) {
		if wf.Name == "bigflow."+name {
			g, err := model.Build(wf)
			if err != nil {
				t.Fatal(err)
			}
			return g, New(g, Options{JSONDecode: true, ArgsCopied: true})
		}
	}
	t.Fatalf("bigflow.%s not found", name)
	return nil, nil
}

// verdictsOf collects the verdict of every stage path that takes the given
// exit of the decision with this text, and none of the exits in not
// ("decision text|exit").
func verdictsOf(reps []StageReport, label, exit string, not ...string) map[Verdict]int {
	out := map[Verdict]int{}
	for _, rep := range reps {
		for _, r := range rep.Results {
			hit, skip := false, false
			for _, s := range r.Path.Steps {
				if s.Junction.Label == label && s.Exit.Label == exit {
					hit = true
				}
				for _, n := range not {
					if s.Junction.Label+"|"+s.Exit.Label == n {
						skip = true
					}
				}
			}
			if hit && !skip {
				out[r.Verdict]++
			}
		}
	}
	return out
}

// Facts across stages: a value the earlier stages leave the same way makes a
// later impossible answer impossible in every incoming state; a value they
// leave in different ways makes an answer possible in one state possible; and
// when there are more incoming states than the limit, an answer that no
// examined state allows is only pending.
func TestStageContexts(t *testing.T) {
	g, a := bigflowWorkflow(t, "ContextsAllImpossible")
	reps := a.AnalyzeStages(g, g.Stages(256), 16)
	if v := verdictsOf(reps, `if mode == "b"`, "true"); v[Impossible] == 0 || v[Possible] != 0 || v[Pending] != 0 {
		t.Errorf("ContextsAllImpossible: verdicts for mode == b, true side: %v (want only impossible)", v)
	}
	if v := verdictsOf(reps, `if mode == "b"`, "false"); v[Possible] == 0 || v[Impossible] != 0 {
		t.Errorf("ContextsAllImpossible: verdicts for mode == b, false side: %v (want only possible)", v)
	}

	g, a = bigflowWorkflow(t, "ContextsDiffer")
	reps = a.AnalyzeStages(g, g.Stages(256), 16)
	if v := verdictsOf(reps, `if v == "x"`, "true", `if v == "y"|true`); v[Possible] == 0 || v[Impossible] != 0 {
		t.Errorf("ContextsDiffer: v == x, true side: %v (want possible: one incoming state has v = x)", v)
	}
	if v := verdictsOf(reps, `if v == "y"`, "true"); v[Impossible] == 0 || v[Possible] != 0 {
		t.Errorf("ContextsDiffer: v == y, true side: %v (want impossible in every state)", v)
	}
	contexts := 0
	for _, r := range reps {
		contexts = max(contexts, r.Contexts)
	}
	if contexts != 2 {
		t.Errorf("ContextsDiffer: %d incoming states, want 2 (v is empty or x)", contexts)
	}

	g, a = bigflowWorkflow(t, "ContextsCutOff")
	cut := a.AnalyzeStages(g, g.Stages(256), 16)
	if v := verdictsOf(cut, `if a1 == "z"`, "true"); v[Pending] == 0 || v[Impossible] != 0 || v[Possible] != 0 {
		t.Errorf("ContextsCutOff with 16: %v (want only pending: 32 states, 16 followed)", v)
	}
	cutOff := false
	for _, r := range cut {
		cutOff = cutOff || r.CutOff
	}
	if !cutOff {
		t.Error("ContextsCutOff with 16: no stage says its states were cut off")
	}
	wide := a.AnalyzeStages(g, g.Stages(256), 64)
	if v := verdictsOf(wide, `if a1 == "z"`, "true"); v[Impossible] == 0 || v[Pending] != 0 {
		t.Errorf("ContextsCutOff with 64: %v (want impossible in every one of the 32 states)", v)
	}

	// The cut is remembered by the stages after the next one: a path that is
	// possible in a state that was left out is never called impossible.
	g, a = bigflowWorkflow(t, "ContextsCutOffLater")
	for _, limit := range []int{4, 16} {
		reps := a.AnalyzeStages(g, g.Stages(256), limit)
		if v := verdictsOf(reps, `if a1 == ""`, "true"); v[Impossible] != 0 {
			t.Errorf("ContextsCutOffLater with %d states: a1 == empty, true side: %v (it is possible in a state that was left out)", limit, v)
		}
	}
	// A stage with an early return leaves one state, but the cut stays.
	g, a = bigflowWorkflow(t, "CutThenReturn")
	if v := verdictsOf(a.AnalyzeStages(g, g.Stages(2), 1), `if sv == "b"`, "true"); v[Impossible] != 0 {
		t.Errorf("CutThenReturn with 1 state: sv == b, true side: %v (possible when a is false, a state that was left out)", v)
	}
	if v := verdictsOf(a.AnalyzeStages(g, g.Stages(2), 16), `if sv == "b"`, "true"); v[Possible] == 0 {
		t.Errorf("CutThenReturn with 16 states: %v (want possible)", v)
	}
	g, a = bigflowWorkflow(t, "ContextsCutOffLater")
	reps = a.AnalyzeStages(g, g.Stages(256), 64)
	if v := verdictsOf(reps, `if a1 == ""`, "true"); v[Possible] == 0 || v[Impossible] != 0 {
		t.Errorf("ContextsCutOffLater with 64 states: %v (want possible)", v)
	}
}
