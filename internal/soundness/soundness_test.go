package soundness

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/instrument"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// fixedSeeds always run. One more seed, chosen at random, runs as well; it
// is printed (and goes into the failure message) so a failure can be repeated.
var fixedSeeds = []int64{1, 2, 3, 4, 5}

// batchSize is how many programs share one package (one compile, one run).
const batchSize = 250

// programCount is how many programs one run checks: 500 normally, 50 with
// -short, and PATHKIT_SOUNDNESS_N when it is set (10,000 for the nightly run).
func programCount(t *testing.T) int {
	if v := os.Getenv("PATHKIT_SOUNDNESS_N"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("PATHKIT_SOUNDNESS_N=%q is not a positive number", v)
		}
		return n
	}
	if testing.Short() {
		return 50
	}
	return 500
}

// observed is what a batch run found out.
type observed struct {
	programs map[string]Program                            // by workflow name
	reached  map[string]map[string]bool                    // workflow -> keys of paths a run took
	paths    map[string][]model.Path                       // workflow -> every listed path
	unmatch  []string                                      // traces that fit no path
	claims   map[string]map[string]classify.Classification // workflow -> path key -> claim
	eng      *classify.Engine                              // the classifier the claims came from (graphs added)
	// helperPaths counts the paths a run took through a followed helper's code.
	helperPaths int
	// unitRuns counts the runs that went through a summarized helper's code.
	unitRuns int
	// listings and plans are what a small path cap and small stages make of each
	// program: the pruned groups, and the class of every stage path.
	listings     map[string]*classify.Listing
	plans        map[string]*classify.StagePlan
	reachedPaths map[string][]model.Path
	graphs       map[string]*model.Graph
	// scenarioErrs are the places where the scenarios disagree with what the
	// runs really did outside the workflow (the Skeptic property); stepRuns
	// counts the runs checked.
	scenarioErrs []string
	// loopRuns counts the runs checked that went through a loop (M11, L1).
	loopRuns     int
	scenarioSeen map[string]string
	stepRuns     int
	// deadlockReruns names the programs that were run again after Temporal's
	// deadlock alarm and then passed every check.
	deadlockReruns []string
}

// runBatch writes the programs into a throw-away module, records the paths
// of every run with PathKit's own instrumentation, and classifies every
// listed path with the real classifier.
func runBatch(t *testing.T, programs []Program) *observed {
	t.Helper()
	root := t.TempDir()
	shapes, err := filepath.Abs("../../testdata/shapes")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(shapes, f))
		if err != nil {
			t.Fatal(err)
		}
		if f == "go.mod" {
			data = []byte(strings.Replace(string(data), "module example.com/shapes", "module example.com/soundness", 1))
		}
		if err := os.WriteFile(filepath.Join(root, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "progs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("progs.go", File(programs))
	write("driver_test.go", DriverTest(programs))

	res, err := load.Load(dir)
	if err != nil {
		t.Fatalf("the generated programs do not compile: %v\n%s", err, aroundError(err.Error(), File(programs)))
	}
	obs := &observed{programs: map[string]Program{}, reached: map[string]map[string]bool{}, paths: map[string][]model.Path{}, claims: map[string]map[string]classify.Classification{}}
	byName := map[string]Program{}
	for _, p := range programs {
		byName["progs."+p.Name] = p
	}
	// PATHKIT_SOUNDNESS_ENGINE=off runs the same programs without the fact
	// engine, to see how many claims it adds.
	// Small stages, few contexts and a small path cap make the programs (about
	// fifty paths each) go through the stage and pruning code.
	eng, err := classify.NewEngine(scope.Classification{Enabled: true, EngineOff: os.Getenv("PATHKIT_SOUNDNESS_ENGINE") == "off", StageMaxPaths: 6, MaxContexts: 3})
	if err != nil {
		t.Fatal(err)
	}
	eng.SetPathCap(12)
	obs.listings, obs.plans, obs.reachedPaths, obs.graphs = map[string]*classify.Listing{}, map[string]*classify.StagePlan{}, map[string][]model.Path{}, map[string]*model.Graph{}
	obs.eng = eng
	graphs := map[string]*model.Graph{}
	hashes := map[string]string{}
	var targets []instrument.Target
	for _, wf := range discover.Find(res.Packages) {
		// Every second program has its helpers summarized (mapped once as
		// units), the others have them copied in: both ways must agree with
		// the recorder and make no false claim.
		mode := model.HelpersInline
		if byName[wf.Name].Number%2 == 0 {
			mode = model.HelpersSummarize
		}
		g, err := model.BuildWith(wf, model.Options{Helpers: mode})
		if err != nil {
			t.Fatalf("%s: %v\n%s", wf.Name, err, byName[wf.Name].Source)
		}
		graphs[wf.Name], hashes[wf.Name] = g, g.CodeHash()
		targets = append(targets, instrument.Target{Workflow: wf, Graph: g})
		if err := eng.AddGraph(wf.Name, g); err != nil {
			t.Fatal(err)
		}
		obs.programs[wf.Name] = byName[wf.Name]
		obs.claims[wf.Name] = map[string]classify.Classification{}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			obs.paths[wf.Name] = append(obs.paths[wf.Name], p)
			obs.claims[wf.Name][p.Key()] = eng.Claim(wf.Name, p)
		}
		obs.listings[wf.Name] = eng.Listing(wf.Name, g)
		obs.plans[wf.Name] = eng.PlanStages(wf.Name, g)
		obs.graphs[wf.Name] = g
	}
	if len(targets) != len(programs) {
		t.Fatalf("%d programs written but PathKit found %d workflows", len(programs), len(targets))
	}

	traces := filepath.Join(root, "traces")
	ins, err := instrument.Instrument(targets, traces)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := instrument.WriteOverlay(ins, filepath.Join(root, "overlay"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-vet=off", "-overlay="+overlay, "-count=1", "./progs")
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(), "PK_TRACES="+traces, "PATHKIT_TESTNAMES=off") // test names are not what this test checks, and the dump costs time inside a workflow // Environ keeps the PWD that follows cmd.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the generated programs failed: %v\n%s", err, firstLines(string(out), 60))
	}

	if data, err := os.ReadFile(filepath.Join(traces, "deadlock-reruns.log")); err == nil {
		for _, l := range strings.Fields(string(data)) {
			obs.deadlockReruns = append(obs.deadlockReruns, l)
			t.Logf("re-run after deadlock alarm: %s (it passed the second time)", l)
		}
	}
	files, err := trace.List(traces)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, p := range programs {
		runs += len(p.Inputs)
	}
	if len(files) != runs { // the recorder ignores write errors, so a lost trace must not go unnoticed
		t.Fatalf("%d runs but %d trace files: a trace was lost, so the check would miss paths", runs, len(files))
	}
	for _, f := range files {
		tf, err := trace.Read(f)
		if err != nil {
			t.Fatal(err)
		}
		out := trace.CheckIn(tf, graphs[tf.Workflow], hashes[tf.Workflow], "example.com/soundness/progs")
		switch out.Kind {
		case trace.Matched:
			if obs.reached[tf.Workflow] == nil {
				obs.reached[tf.Workflow] = map[string]bool{}
			}
			obs.reached[tf.Workflow][out.Path.Key()] = true
			obs.reachedPaths[tf.Workflow] = append(obs.reachedPaths[tf.Workflow], out.Path)
			if len(graphs[tf.Workflow].UnitHits(out.Steps)) > 0 {
				obs.unitRuns++
			}
			obs.checkScenario(graphs[tf.Workflow], tf, f, out.Path)
			for _, st := range out.Path.Steps {
				if st.Junction.Site != nil {
					obs.helperPaths++
					break
				}
			}
		default:
			obs.unmatch = append(obs.unmatch, fmt.Sprintf("%s: %v (%s)", tf.Workflow, tf.Steps, out.Reason))
		}
	}
	return obs
}

func firstLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.Contains(l, " DEBUG ") { // the Temporal test log drowns the real message
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}

// falseClaims lists every path that claim says cannot be tested but a run took.
func (o *observed) falseClaims(claim func(workflow string, p model.Path) classify.Classification) []string {
	var out []string
	for wf, paths := range o.paths {
		for _, p := range paths {
			if o.reached[wf][p.Key()] && claim(wf, p).Bucket == classify.CannotBeTested {
				c := claim(wf, p)
				prog := o.programs[wf]
				out = append(out, fmt.Sprintf("FALSE CLAIM: %s (seed %d, program %d) path %s was run, but is called %s (%s): %s\n%s",
					wf, prog.Seed, prog.Number, p.Key(), c.Bucket, c.Label, c.Explanation, prog.Source))
			}
		}
	}
	sort.Strings(out)
	return out
}

// stageFalseClaims lists every place where a path a run took lies in a part of
// the map the stages or the pruning call impossible: a pruned group the path
// runs through, or a stage path the stage plan calls cannot-be-tested.
func (o *observed) stageFalseClaims() (out []string, stageClaims, prunedGroups int) {
	for wf, plan := range o.plans {
		for k := range plan.Classes {
			for _, sc := range plan.Classes[k] {
				if sc.Class.Bucket == classify.CannotBeTested {
					stageClaims++
				}
			}
		}
		prunedGroups += len(o.listings[wf].Pruned)
		prog := o.programs[wf]
		for _, p := range o.reachedPaths[wf] {
			for gi, pg := range o.listings[wf].Pruned {
				if len(p.Steps) >= len(pg.Group.Prefix) {
					in := true
					for i, s := range pg.Group.Prefix {
						in = in && p.Steps[i].Exit == s.Exit
					}
					if in {
						out = append(out, fmt.Sprintf("FALSE PRUNED GROUP %d: %s (seed %d, program %d) path %s was run, but is inside a part left out as impossible: %s\n%s", gi, wf, prog.Seed, prog.Number, p.Key(), pg.Class.Explanation, prog.Source))
					}
				}
			}
			stages := o.plans[wf].Stages
			for _, pc := range o.graphs[wf].StagePieces(p, stages) {
				for _, sc := range o.plans[wf].Classes[pc.Stage] {
					if sc.Path.Key() == pc.Key() && sc.Class.Bucket == classify.CannotBeTested {
						out = append(out, fmt.Sprintf("FALSE STAGE CLAIM: %s (seed %d, program %d) path %s was run, but its stage path %s is called %s (%s): %s\n%s", wf, prog.Seed, prog.Number, p.Key(), pc.Key(), sc.Class.Bucket, sc.Class.Label, sc.Class.Explanation, prog.Source))
					}
				}
			}
		}
	}
	sort.Strings(out)
	return
}

// stats counts what the run covered, so a green run can be told from one
// that checked nothing.
func (o *observed) stats() (programs, paths, reached, claimed int) {
	programs = len(o.programs)
	for wf, ps := range o.paths {
		paths += len(ps)
		reached += len(o.reached[wf])
		for _, c := range o.claims[wf] {
			if c.Bucket == classify.CannotBeTested {
				claimed++
			}
		}
	}
	return
}

func TestNoFalseCannotBeTestedClaims(t *testing.T) {
	n := programCount(t)
	random := rand.Int63n(1_000_000) + 100 // never one of the fixed seeds
	seeds := append(append([]int64{}, fixedSeeds...), random)
	t.Logf("%d programs; fixed seeds %v plus random seed %d (to repeat a failure: PATHKIT_SOUNDNESS_SEED=%d)", n, fixedSeeds, random, random)
	if v := os.Getenv("PATHKIT_SOUNDNESS_SEED"); v != "" {
		s, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("PATHKIT_SOUNDNESS_SEED=%q is not a number", v)
		}
		seeds = []int64{s}
	}
	// PATHKIT_SOUNDNESS_SEEDS=1,2,3 runs exactly these seeds (the nightly job uses
	// it to run the fixed seeds alone, and PATHKIT_SOUNDNESS_SEED for tonight's).
	if v := os.Getenv("PATHKIT_SOUNDNESS_SEEDS"); v != "" {
		seeds = nil
		for _, f := range strings.Split(v, ",") {
			s, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
			if err != nil {
				t.Fatalf("PATHKIT_SOUNDNESS_SEEDS=%q is not a list of numbers", v)
			}
			seeds = append(seeds, s)
		}
	}
	t.Logf("SEEDS USED: %v", seeds)

	var all []Program
	per := (n + len(seeds) - 1) / len(seeds)
	for _, s := range seeds {
		for i := 0; i < per; i++ {
			all = append(all, Generate(s, i+1))
		}
	}

	var totalPrograms, totalPaths, totalReached, totalClaimed, falseCount, helperPaths, stageClaims, prunedGroups, unitRuns, scenarioErrors, stepRuns, loopRuns, deadlockReruns int
	for start := 0; start < len(all); start += batchSize {
		end := min(start+batchSize, len(all))
		obs := runBatch(t, all[start:end])
		if len(obs.unmatch) > 0 {
			t.Errorf("%d recorded runs fit no path (the map and the recorder disagree); first: %s", len(obs.unmatch), obs.unmatch[0])
		}
		for _, f := range obs.falseClaims(obs.eng.Claim) {
			falseCount++
			t.Error(f)
		}
		for _, e := range obs.scenarioErrs {
			scenarioErrors++
			t.Error(e)
		}
		stepRuns += obs.stepRuns
		loopRuns += obs.loopRuns
		sf, sc, pg := obs.stageFalseClaims()
		for _, f := range sf {
			falseCount++
			t.Error(f)
		}
		stageClaims, prunedGroups = stageClaims+sc, prunedGroups+pg
		helperPaths += obs.helperPaths
		unitRuns += obs.unitRuns
		deadlockReruns += len(obs.deadlockReruns)
		p, ps, r, c := obs.stats()
		totalPrograms, totalPaths, totalReached, totalClaimed = totalPrograms+p, totalPaths+ps, totalReached+r, totalClaimed+c
	}
	t.Logf("DEADLOCK ALARM RE-RUNS: %d program(s) were run again after Temporal's deadlock alarm and passed every check the second time (a second alarm would have failed the run)", deadlockReruns)
	t.Logf("SOUNDNESS: %d programs run, %d paths listed, %d paths really taken, %d paths claimed cannot-be-tested, %d false claims (%d runs went through helper code, %d through summarized helpers; %d stage paths and %d pruned groups were checked too)", totalPrograms, totalPaths, totalReached, totalClaimed, falseCount, helperPaths, unitRuns, stageClaims, prunedGroups)
	if stageClaims == 0 || prunedGroups == 0 {
		t.Errorf("the stage and pruning code was not exercised: %d stage claims, %d pruned groups", stageClaims, prunedGroups)
	}
	if unitRuns == 0 && n >= 100 {
		t.Error("no run went through the code of a summarized helper: the summarized helpers were not exercised")
	}
	if helperPaths == 0 {
		t.Error("no run went through the code of a helper function: the helper probes were not exercised")
	}
	t.Logf("SCENARIOS: %d runs checked against their scenario's external steps (%d of them through a loop), %d disagreements", stepRuns, loopRuns, scenarioErrors)
	if loopRuns == 0 && n >= 200 {
		t.Error("no run through a loop was checked against its scenario: the loop-aware check looked at nothing")
	}
	if stepRuns == 0 && n >= 50 {
		t.Error("no run was checked against its scenario: the scenario check looked at nothing")
	}
	if totalReached == 0 {
		t.Error("no path was recorded as taken: the harness checked nothing")
	}
	if totalClaimed == 0 {
		t.Error("PathKit claimed no path impossible in any program: the claims were not exercised (generator or classifier changed?)")
	}
}

// The harness has teeth: a classifier that deliberately calls a reachable
// path impossible is caught.
func TestHarnessCatchesAWrongClaim(t *testing.T) {
	var programs []Program
	for i := 1; i <= 20; i++ {
		programs = append(programs, Generate(1, i))
	}
	obs := runBatch(t, programs)
	wrong := func(wf string, p model.Path) classify.Classification {
		c := classify.DefaultTestable()
		if len(p.Steps) > 0 { // "every path that makes a decision is impossible"
			c.Bucket, c.Label = classify.CannotBeTested, "contradictory"
		}
		return c
	}
	if got := obs.falseClaims(wrong); len(got) == 0 {
		t.Fatal("a classifier that calls reachable paths impossible was not caught")
	}
	if _, _, reached, _ := obs.stats(); reached == 0 {
		t.Fatal("no path was recorded as taken")
	}
}

// The generator is deterministic and its programs have decisions.
func TestGeneratorIsDeterministic(t *testing.T) {
	for i := 1; i <= 50; i++ {
		a, b := Generate(7, i), Generate(7, i)
		if a.Source != b.Source || strings.Join(a.Inputs, "|") != strings.Join(b.Inputs, "|") {
			t.Fatalf("program %d differs between two calls", i)
		}
		decisions := strings.Count(a.Source, "if ") + strings.Count(a.Source, "switch ") + strings.Count(a.Source, "for ")
		if decisions < 3 {
			t.Errorf("program %d has only %d decisions:\n%s", i, decisions, a.Source)
		}
	}
}

// The ground truth is complete: for a program whose four paths can all be
// taken, all four are recorded, and the real classifier calls none impossible.
func TestGroundTruthIsComplete(t *testing.T) {
	p := Program{Seed: 0, Number: 1, Name: "ProgKnown", Inputs: []string{"false, false", "false, true", "true, false", "true, true"},
		Source: "func ProgKnown(ctx workflow.Context, b1 bool, b2 bool) error {\n\tif b1 {\n\t\tnoop()\n\t}\n\tif b2 {\n\t\tnoop()\n\t}\n\treturn nil\n}\n"}
	obs := runBatch(t, []Program{p})
	if _, paths, reached, claimed := obs.stats(); paths != 4 || reached != 4 || claimed != 0 {
		t.Errorf("want 4 paths, all 4 taken and none claimed impossible; got %d paths, %d taken, %d claimed", paths, reached, claimed)
	}
}

// scenarioToken is what one step of a fingerprint makes in the run's event list:
// "A:Name" for an activity, "L:Name" for a local activity, "T" for a timer, or
// "" for a step that makes none.
func scenarioToken(st string) string {
	switch {
	case strings.Contains(st, " (activity)"):
		return "A:" + st[:strings.Index(st, " (activity)")]
	case strings.Contains(st, " (local activity)"):
		return "L:" + st[:strings.Index(st, " (local activity)")]
	case strings.HasPrefix(st, "timer"), strings.Contains(st, "(timer)"), strings.HasPrefix(st, "AwaitWithTimeout (wait)"), strings.HasPrefix(st, "wait with timeout"):
		return "T"
	}
	return ""
}

// scenarioPattern turns a path's fingerprint into the pattern a run's event list
// must match: every external step of the path, in order, exactly; and, at the
// place of each loop, any steps (the rounds before the last one, which are not on
// the path). Nothing else may differ: an extra step before or after the loop, or
// between two other steps, does not match. The end label is the last step.
func scenarioPattern(steps []string, spans []model.Span) *regexp.Regexp {
	token := scenarioToken
	used := map[int]bool{} // spans already turned into a pattern (a span starts where its first step is)
	var build func(lo, hi int) string
	build = func(lo, hi int) string {
		var b strings.Builder
		i := lo
		for i < hi {
			// the outermost span that starts here
			si := -1
			for k, sp := range spans {
				if !used[k] && sp.From == i && sp.To <= hi && sp.To >= sp.From && (si < 0 || sp.To > spans[si].To) {
					si = k
				}
			}
			if si >= 0 && spans[si].To >= spans[si].From {
				// a loop whose last round makes no external call still had earlier
				// rounds that may have made some: an empty span is a span too
				used[si] = true
				b.WriteString("(?:[^\\n]+\\n)*") // the earlier rounds: any steps
				b.WriteString(build(spans[si].From, spans[si].To))
				i = max(i, spans[si].To)
				continue
			}
			if t := token(steps[i]); t != "" {
				b.WriteString(regexp.QuoteMeta(t) + "\\n")
			}
			i++
		}
		// an empty loop span that starts where this range ends (the loop's last
		// round is empty and nothing comes after it but the end)
		for k, sp := range spans {
			if !used[k] && sp.From == hi && sp.To == hi && lo < hi {
				used[k] = true
				b.WriteString("(?:[^\\n]+\\n)*")
			}
		}
		return b.String()
	}
	return regexp.MustCompile("^" + build(0, len(steps)-1) + "$")
}

// checkScenario checks a run against its scenario (docs/DESIGN.md D25): the
// external steps the path says (its fingerprint) must be the ones the run really
// made (activities, local activities, timers, in order), and two runs of one
// scenario must have made the same ones. Only for programs whose helpers are
// copied in: a summarized helper's own steps are not part of the path.
func (o *observed) checkScenario(g *model.Graph, tf trace.File, file string, p model.Path) {
	if g.Summarized() {
		return
	}
	raw, err := os.ReadFile(file + ".events")
	if err != nil {
		return
	}
	actual := strings.TrimSpace(string(raw))
	fp := g.Fingerprint(p)
	if strings.HasPrefix(fp[len(fp)-1], "unknown ") {
		return // a place PathKit did not read: the path has a scenario of its own
	}
	// A loop is read once per path: the steps of its last round. The rounds before
	// it are not on the path, so the run's steps are normalized the same way: what
	// comes before the loop, any steps for the earlier rounds, the last round, what
	// comes after (see FingerprintSpans).
	steps, spans := g.FingerprintSpans(p)
	pattern := scenarioPattern(steps, spans)
	token := scenarioToken
	loops := len(spans) > 0
	run := actual
	if run != "" {
		run += "\n"
	}
	o.stepRuns++
	if !pattern.MatchString(run) {
		said := ""
		for _, st := range steps {
			if t := token(st); t != "" {
				said += t + " "
			}
		}
		o.scenarioErrs = append(o.scenarioErrs, fmt.Sprintf("%s: path %s says the run makes %q (loops: %v), the run made %q\n%s\n%s", tf.Workflow, p.Key(), said, loops, actual, strings.Join(fp, " | "), firstLines(o.programs[tf.Workflow].Source, 70)))
		return
	}
	if loops {
		o.loopRuns++
		return // runs of one scenario may differ in how many rounds they made
	}
	if o.scenarioSeen == nil {
		o.scenarioSeen = map[string]string{}
	}
	id := tf.Workflow + "/" + model.ScenarioID(fp)
	if prev, ok := o.scenarioSeen[id]; ok && prev != actual {
		o.scenarioErrs = append(o.scenarioErrs, fmt.Sprintf("%s: two runs of scenario %s made different steps: %q and %q", tf.Workflow, id, prev, actual))
	}
	o.scenarioSeen[id] = actual
}

// aroundError shows the lines of src near the line a compile error names
// ("progs.go:7528:37"), or the first lines when there is none.
func aroundError(msg, src string) string {
	m := regexp.MustCompile(`progs\.go:(\d+):`).FindStringSubmatch(msg)
	if m == nil {
		return firstLines(src, 40)
	}
	n, _ := strconv.Atoi(m[1])
	lines := strings.Split(src, "\n")
	lo, hi := max(0, n-6), min(len(lines), n+4)
	return strings.Join(lines[lo:hi], "\n")
}

// A program the 10,000-program run found: a loop whose last round makes no
// external call (a loop over two values whose body only decides), where an
// earlier round called a helper that sleeps. The scenario check used to reject
// the run because it only allowed earlier rounds before a non-empty last round.
func TestLoopWithEmptyLastRound(t *testing.T) {
	obs := runBatch(t, []Program{Generate(2, 823)})
	if len(obs.unmatch) > 0 {
		t.Errorf("runs fit no path: %s", obs.unmatch[0])
	}
	for _, f := range obs.falseClaims(obs.eng.Claim) {
		t.Error(f)
	}
	for _, e := range obs.scenarioErrs {
		t.Error(e)
	}
	if obs.stepRuns == 0 {
		t.Error("no run was checked against its scenario")
	}
}

// The relaxed check lets an empty loop span stand for earlier rounds of that
// loop and nothing else: a step outside the loop must still be caught.
func TestScenarioPatternAllowsExtraStepsOnlyAtTheLoop(t *testing.T) {
	pre, post := "Pre (activity)", "Post (activity)"
	const loop = "loop for i < 2: 1+ rounds"
	// Pre, the loop (its last round makes nothing), Post
	steps := []string{pre, loop, post, "end: completed"}
	spans := []model.Span{{From: 2, To: 2}}
	re := scenarioPattern(steps, spans)
	for run, want := range map[string]bool{
		"A:Pre\nA:Post\n":             true,  // the loop ran with no external step
		"A:Pre\nT\nA:Post\n":          true,  // an earlier round slept: inside the loop
		"A:Pre\nA:Other\nT\nA:Post\n": true,  // two steps of earlier rounds
		"A:Extra\nA:Pre\nA:Post\n":    false, // an extra step before Pre
		"A:Pre\nA:Post\nA:Extra\n":    false, // an extra step after Post
		"A:Pre\nA:Post\nT\n":          false, // a timer after the loop
		"A:Post\n":                    false, // a step is missing
		"A:Pre\n":                     false, // a step is missing
		"A:Post\nA:Pre\n":             false, // wrong order
	} {
		if got := re.MatchString(run); got != want {
			t.Errorf("run %q: matches = %v, want %v", run, got, want)
		}
	}
	// the loop is the last thing the path does before its end
	steps = []string{pre, loop, "end: completed"}
	re = scenarioPattern(steps, []model.Span{{From: 2, To: 2}})
	for run, want := range map[string]bool{
		"A:Pre\n":           true,
		"A:Pre\nT\n":        true,  // an earlier round slept
		"A:Extra\nA:Pre\n":  false, // before the first step
		"T\nA:Pre\n":        false,
		"A:Pre\nA:Pre\nT\n": true, // after Pre, at the loop: earlier rounds
		"":                  false,
	} {
		if got := re.MatchString(run); got != want {
			t.Errorf("loop last, run %q: matches = %v, want %v", run, got, want)
		}
	}
	// near miss: without a loop span the same extra step is refused
	if scenarioPattern([]string{pre, post, "end: completed"}, nil).MatchString("A:Pre\nT\nA:Post\n") {
		t.Error("without a loop nothing may come between two steps")
	}
}

// A program the 10,000-program run found (seed 5, program 1371): a map was made
// empty, handed to a plain function that writes into it, and a later check of
// its length was called impossible from the empty start.
func TestLengthAfterAPlainWriter(t *testing.T) {
	obs := runBatch(t, []Program{Generate(5, 1371)})
	if len(obs.unmatch) > 0 {
		t.Errorf("runs fit no path: %s", obs.unmatch[0])
	}
	if f := obs.falseClaims(obs.eng.Claim); len(f) > 0 {
		t.Errorf("%d false claim(s); first: %s", len(f), f[0])
	}
	if sf, _, _ := obs.stageFalseClaims(); len(sf) > 0 {
		t.Errorf("%d false stage claim(s); first: %s", len(sf), sf[0])
	}
	for _, e := range obs.scenarioErrs {
		t.Error(e)
	}
}

// Only Temporal's deadlock alarm earns a second attempt of a generated program;
// a second alarm, and every other error, is a failure. The test compiles and
// runs the very text the generated driver contains (retrySource).
func TestRetryPolicyOnlyForTheDeadlockAlarm(t *testing.T) {
	const alarm = "workflow execution error: [TMPRL1101] Potential deadlock detected: workflow goroutine \"root\" didn't yield for over 1s"
	dir := t.TempDir()
	main := "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n" + retrySource + `
func main() {
	try := func(name string, results ...string) {
		calls, notes := 0, 0
		got := runWithRetry(func() string {
			r := results[calls]
			calls++
			return r
		}, func() { notes++ })
		fmt.Printf("%s calls=%d notes=%d result=%q\n", name, calls, notes, got)
	}
	const alarm = ` + strconv.Quote(alarm) + `
	try("clean", "", "unused")
	try("alarm-then-pass", alarm, "")
	try("alarm-twice", alarm, alarm)
	try("other-error", "boom", "unused")
	try("not-completed", "not completed", "unused")
	try("alarm-then-other", alarm, "boom")
	try("other-then-alarm", "boom", alarm)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module retrytest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		`clean calls=1 notes=0 result=""`,
		`alarm-then-pass calls=2 notes=1 result=""`,
		`alarm-twice calls=2 notes=1 result=` + strconv.Quote(alarm), // still a failure
		`other-error calls=1 notes=0 result="boom"`,                  // no second attempt
		`not-completed calls=1 notes=0 result="not completed"`,
		`alarm-then-other calls=2 notes=1 result="boom"`, // the second attempt's error counts
		`other-then-alarm calls=1 notes=0 result="boom"`, // only the first error decides
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing line %q in:\n%s", want, got)
		}
	}
	// the generated driver contains exactly this rule and the longer alarm time
	d := DriverTest([]Program{Generate(1, 1)})
	if !strings.Contains(d, strings.TrimSpace(retrySource)) || !strings.Contains(d, "DeadlockDetectionTimeout: 5 * time.Second") {
		t.Error("the generated driver does not hold the retry rule and the 5 second alarm time")
	}
}

// Programs the 10,000-program runs have flagged once (the seed and program
// number), kept so every CI run checks them again on a clean machine:
//   - seed 2, program 823: a scenario check on a loop with an empty last round
//     (fixed in the checker);
//   - seed 5, program 1371: a false "can't happen" claim after a map was handed
//     to a plain function that writes into it (fixed in the engine);
//   - seed 1, program 49: a false "can't happen" claim after a struct that holds a
//     map was handed to a function that fills the map (fixed in the engine);
//   - seed 4, program 612: Temporal's deadlock alarm once in a run of 10,000,
//     never again on its own (a stalled machine; see the retry rule).
//
// To check one of them on another machine: PATHKIT_SOUNDNESS_SEED=<seed>
// PATHKIT_SOUNDNESS_N=<program number + 1> go test ./internal/soundness -run
// TestNoFalseCannotBeTestedClaims -count=1
func TestKnownPrograms(t *testing.T) {
	known := []struct{ seed, program int }{{2, 823}, {5, 1371}, {4, 612}, {1, 49}}
	var progs []Program
	for _, k := range known {
		progs = append(progs, Generate(int64(k.seed), k.program))
	}
	obs := runBatch(t, progs)
	if len(obs.unmatch) > 0 {
		t.Errorf("runs fit no path: %s", obs.unmatch[0])
	}
	for _, f := range obs.falseClaims(obs.eng.Claim) {
		t.Error(f)
	}
	if sf, _, _ := obs.stageFalseClaims(); len(sf) > 0 {
		t.Errorf("%d false stage claim(s); first: %s", len(sf), sf[0])
	}
	for _, e := range obs.scenarioErrs {
		t.Error(e)
	}
	if obs.stepRuns == 0 {
		t.Error("no run was checked against its scenario")
	}
}

// Programs of different seeds with the same number are written into one package
// in one batch: the names the hand-off blocks declare must not collide (found
// when the default test, with 500 programs over six seeds, did not compile).
func TestHandoffNamesDoNotCollideAcrossSeeds(t *testing.T) {
	var progs []Program
	for seed := int64(1); seed <= 3; seed++ {
		for number := 7; number <= 70; number += 7 { // every seventh program has the blocks
			progs = append(progs, Generate(seed, number))
		}
	}
	names := map[string]bool{}
	for _, p := range progs {
		for _, decl := range []string{"type holder", "type pholder", "func growStrs", "func fillHolder", "func fillPHolder"} {
			for _, line := range strings.Split(p.Source, "\n") {
				if strings.HasPrefix(line, decl) {
					name := strings.Fields(line)[1]
					if i := strings.IndexAny(name, "("); i >= 0 {
						name = name[:i]
					}
					if names[name] {
						t.Fatalf("%s declared twice in one batch (program %s)", name, p.Name)
					}
					names[name] = true
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no hand-off block found: the test checks nothing")
	}
	obs := runBatch(t, progs) // and the whole batch compiles and runs
	if f := obs.falseClaims(obs.eng.Claim); len(f) > 0 {
		t.Errorf("%d false claim(s); first: %s", len(f), f[0])
	}
}
