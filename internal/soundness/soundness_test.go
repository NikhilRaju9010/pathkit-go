package soundness

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
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
		t.Fatalf("the generated programs do not compile: %v\n%s", err, firstLines(File(programs), 40))
	}
	obs := &observed{programs: map[string]Program{}, reached: map[string]map[string]bool{}, paths: map[string][]model.Path{}, claims: map[string]map[string]classify.Classification{}}
	byName := map[string]Program{}
	for _, p := range programs {
		byName["progs."+p.Name] = p
	}
	// PATHKIT_SOUNDNESS_ENGINE=off runs the same programs without the fact
	// engine, to see how many claims it adds.
	eng, err := classify.NewEngine(scope.Classification{Enabled: true, EngineOff: os.Getenv("PATHKIT_SOUNDNESS_ENGINE") == "off"})
	if err != nil {
		t.Fatal(err)
	}
	obs.eng = eng
	graphs := map[string]*model.Graph{}
	hashes := map[string]string{}
	var targets []instrument.Target
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.Build(wf)
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
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the generated programs failed: %v\n%s", err, firstLines(string(out), 60))
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

	var all []Program
	per := (n + len(seeds) - 1) / len(seeds)
	for _, s := range seeds {
		for i := 0; i < per; i++ {
			all = append(all, Generate(s, i+1))
		}
	}

	var totalPrograms, totalPaths, totalReached, totalClaimed, falseCount, helperPaths int
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
		helperPaths += obs.helperPaths
		p, ps, r, c := obs.stats()
		totalPrograms, totalPaths, totalReached, totalClaimed = totalPrograms+p, totalPaths+ps, totalReached+r, totalClaimed+c
	}
	t.Logf("SOUNDNESS: %d programs run, %d paths listed, %d paths really taken, %d paths claimed cannot-be-tested, %d false claims (%d runs went through helper code)", totalPrograms, totalPaths, totalReached, totalClaimed, falseCount, helperPaths)
	if helperPaths == 0 {
		t.Error("no run went through the code of a helper function: the helper probes were not exercised")
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
