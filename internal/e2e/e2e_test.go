// Package e2e runs "pathkit test" for real (go test with the overlay) on
// the pilot project and checks each test's recorded trace lands on the
// path the hand-written answer key (testdata/pilot/EXPECTED.md) names.
//
// RULE: EXPECTED.md is never edited to make these tests pass. If PathKit
// and the key disagree and the key looks wrong, stop and ask the owner.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/cli"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/expected"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

func abs(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// pathkit runs the CLI in-process and returns its output and exit code.
func pathkit(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// hashTree fingerprints every file under dir, to prove nothing changed.
func hashTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	sums := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		sums[path] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sums
}

type current struct {
	graph *model.Graph
	hash  string
}

// graphs builds the current graph and hash of every workflow PathKit can
// map under pattern.
func graphs(t *testing.T, pattern string) map[string]current {
	t.Helper()
	res, err := load.Load(pattern)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]current{}
	for _, wf := range discover.Find(res.Packages) {
		if g, err := model.Build(wf); err == nil {
			out[wf.Name] = current{g, model.FunctionHash(wf.Pkg.Fset, wf.Func)}
		}
	}
	return out
}

func readTraces(t *testing.T, dir string) []trace.File {
	t.Helper()
	paths, err := trace.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []trace.File
	for _, p := range paths {
		f, err := trace.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

func TestAnswerKey(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on the pilot; skipped with -short")
	}
	pilot := abs(t, "../../testdata/pilot")
	key, err := expected.Read(filepath.Join(pilot, "EXPECTED.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Every workflow and test comes from the key. A key workflow that
	// PathKit skips (or can't find) fails here, before any test runs.
	res, err := load.Load(pilot + "/...")
	if err != nil {
		t.Fatal(err)
	}
	workflows := map[string]discover.Workflow{}
	for _, wf := range discover.Find(res.Packages) {
		workflows[wf.Name] = wf
	}
	now := map[string]current{}
	for _, name := range expected.Names(key) {
		g, err := expected.Build(workflows, name)
		if err != nil {
			t.Fatal(err)
		}
		now[name] = current{g, model.FunctionHash(workflows[name].Pkg.Fset, workflows[name].Func)}
	}
	wantTests := expected.TestCount(key)
	if wantTests != 21 {
		t.Fatalf("EXPECTED.md lists %d tests, want 21", wantTests)
	}
	before := hashTree(t, pilot)
	t.Chdir(t.TempDir()) // pathkit writes .pathkit/ here, never into the pilot

	checked := 0
	for _, name := range expected.Names(key) {
		ew := key[name]
		if len(ew.Tests) == 0 {
			t.Fatalf("EXPECTED.md has no tests listed for %s", name)
		}
		pkgDir := filepath.Join(pilot, filepath.Dir(ew.File))
		for _, test := range ew.Tests {
			traces := t.TempDir()
			stdout, stderr, code := pathkit(t, "test", pkgDir, "--traces", traces, "--", "-run", "^"+test.Name+"$")
			if code != 0 {
				t.Fatalf("pathkit test (%s) exit %d\nstdout:\n%s\nstderr:\n%s", test.Name, code, stdout, stderr)
			}
			files := readTraces(t, traces)
			if len(files) != 1 || files[0].Workflow != name {
				t.Fatalf("%s: want exactly one trace for %s, got %+v", test.Name, name, files)
			}
			out := trace.Check(files[0], now[name].graph, now[name].hash)
			wantPath := ew.Paths[test.Path-1]
			want := withNote(wantPath.Key, wantPath.Compensation)
			if got := withNote(out.Path.Key(), out.Path.Compensation); out.Kind != trace.Matched || got != want {
				t.Errorf("%s: trace %v → %s %q (%s); EXPECTED.md says path %d = %q",
					test.Name, files[0].Steps, out.Kind, got, out.Reason, test.Path, want)
				continue
			}
			t.Logf("%s → EXPECTED path %d (%s) ✓", test.Name, test.Path, want)
			checked++
		}
	}
	if checked != wantTests {
		t.Errorf("%d of the %d tests in EXPECTED.md landed on their path", checked, wantTests)
	}

	after := hashTree(t, pilot)
	if len(before) != len(after) {
		t.Errorf("pilot had %d files before and %d after", len(before), len(after))
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("pilot file changed or removed: %s", path)
		}
	}
}

func TestTraceClearingAndRepeatRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test; skipped with -short")
	}
	orders := abs(t, "../../testdata/pilot/orders")
	t.Chdir(t.TempDir())
	traces := abs(t, "traces")
	if err := os.MkdirAll(traces, 0o755); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(traces, "notes.txt")
	if err := os.WriteFile(notes, []byte("not a trace"), 0o644); err != nil {
		t.Fatal(err)
	}

	count := func(args ...string) int {
		t.Helper()
		_, stderr, code := pathkit(t, append([]string{"test", orders, "--traces", traces}, args...)...)
		if code != 0 {
			t.Fatalf("pathkit test %v: exit %d\n%s", args, code, stderr)
		}
		return len(readTraces(t, traces))
	}
	if n := count("--", "-run", "^TestOrderRejected$"); n != 1 {
		t.Errorf("first run: %d traces, want 1", n)
	}
	if n := count("--keep-traces", "--", "-run", "^TestOrderCharged$"); n != 2 {
		t.Errorf("--keep-traces: %d traces, want 2 (old one kept)", n)
	}
	// Same command as the first run: Go's test cache must not skip the
	// tests (that would record nothing), and old traces are cleared.
	if n := count("--", "-run", "^TestOrderRejected$"); n != 1 {
		t.Errorf("repeat run: %d traces, want 1 (cleared, then re-recorded)", n)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("clearing deleted a file that is not a trace: %v", err)
	}
}

func TestFixturesUnderOverlay(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test; skipped with -short")
	}
	fixtures := abs(t, "../../testdata/fixtures")
	t.Chdir(t.TempDir())

	// R3: the replay-probe test must pass under pathkit test too. It runs
	// the workflow 3 times, so there must be exactly 3 complete traces with
	// identical steps: one per run, nothing recorded twice.
	traces := t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", fixtures+"/replay", "--traces", traces); code != 0 {
		t.Fatalf("replay fixture: exit %d\n%s\n%s", code, stdout, stderr)
	}
	files := readTraces(t, traces)
	if len(files) != 3 {
		t.Fatalf("replay fixture: want 3 traces (3 runs), got %d", len(files))
	}
	for _, f := range files {
		if f.Status != "complete" || !slices.Equal(f.Steps, files[0].Steps) {
			t.Errorf("replay fixture: trace %+v differs from the first one %v", f, files[0].Steps)
		}
	}

	// R2: the panic still points at the real line (checked by the fixture's
	// own test), and the panicking run's trace is marked incomplete.
	traces = t.TempDir()
	if stdout, stderr, code := pathkit(t, "test", fixtures+"/panics", "--traces", traces); code != 0 {
		t.Fatalf("panics fixture: exit %d\n%s\n%s", code, stdout, stderr)
	}
	// The panic happens while the return value is computed, so the run
	// must NOT count as complete (the TS d0aa17a trap).
	if files := readTraces(t, traces); len(files) != 1 || files[0].Status != "incomplete" {
		t.Errorf("panics fixture: want 1 incomplete trace, got %+v", files)
	}
}

// withNote is a path's key plus its compensation note, when it has one.
func withNote(key string, compensation bool) string {
	if compensation {
		return key + " " + model.CompensationNote
	}
	return key
}

// liveCase is one fixture test and the path its single run must take.
type liveCase struct {
	test, workflow, want string
}

// liveFixtures are fixture packages whose tests run for real under
// "pathkit test"; each test runs one workflow once.
var liveFixtures = map[string][]liveCase{
	"switches": {
		// fallthrough from "huge" into "large" records no second case
		{"TestRouteHugeFallsThrough", "switches.RouteWorkflow", `J1.success J2.case "huge" J3.false|completed`},
		{"TestRouteLarge", "switches.RouteWorkflow", `J1.success J2.case "large" J3.false|completed`},
		{"TestRouteTiny", "switches.RouteWorkflow", `J1.success J2.case "small", "tiny" J3.false|completed`},
		// no case matches: the default PathKit added
		{"TestRouteMediumHasNoLane", "switches.RouteWorkflow", "J1.success J2.default J3.true|failed"},
		{"TestRouteMeasureFails", "switches.RouteWorkflow", "J1.failure|failed"},
		{"TestLedgerRefund", "switches.LedgerWorkflow", "J1.case Refund|completed"},
		{"TestLedgerCharge", "switches.LedgerWorkflow", "J1.case Charge|completed"},
		{"TestLedgerUnknown", "switches.LedgerWorkflow", "J1.default|failed"},
	},
	// Several trips are folded by the loop rule: only the last trip is kept.
	"loops": {
		{"TestNestedNoTrips", "loops.NestedWorkflow", "J1.exit|completed"},
		// 3 outer trips, each with 2 inner trips
		{"TestNestedManyTrips", "loops.NestedWorkflow", "J1.iterate J2.iterate J3.success J2.retry J2.exit J1.retry J1.exit|completed"},
		// fails on the 2nd inner trip of the 3rd outer trip
		{"TestNestedFailsLate", "loops.NestedWorkflow", "J1.iterate J2.iterate J3.failure|failed"},
		{"TestNestedInnerEmpty", "loops.NestedWorkflow", "J1.iterate J2.exit J1.retry J1.exit|completed"},
		// skips a zero (continue), checks 5, finds 7 (break: no exit step)
		{"TestScanFindsAfterSkips", "loops.ScanWorkflow", "J1.iterate J2.false J3.success J4.true|completed"},
		{"TestScanNothing", "loops.ScanWorkflow", "J1.iterate J2.false J3.success J4.false J1.retry J1.exit|completed"},
		{"TestScanEmpty", "loops.ScanWorkflow", "J1.exit|completed"},
		{"TestScanOnlyZeros", "loops.ScanWorkflow", "J1.iterate J2.true J1.retry J1.exit|completed"},
		{"TestScanCheckFails", "loops.ScanWorkflow", "J1.iterate J2.false J3.failure|failed"},
		// for {}: 3 trips, left by break
		{"TestWaitThreeHours", "loops.WaitWorkflow", "J1.iterate J2.success J3.true|completed"},
		// continue outer on the first row, the second row runs out
		{"TestGridSkipsRowThenFinishes", "loops.GridWorkflow", "J1.iterate J2.iterate J3.false J4.false J5.success J2.retry J2.exit J1.retry J1.exit|completed"},
		// break outer on the second row: neither loop records an exit
		{"TestGridStopsAtZero", "loops.GridWorkflow", "J1.iterate J2.iterate J3.false J4.true|completed"},
		// transparent loop: 3 items, nothing recorded for the loop
		{"TestSumManyItems", "loops.SumWorkflow", "J1.true|completed"},
		{"TestSumNoItems", "loops.SumWorkflow", "J1.false|completed"},
		// no Temporal call but an if inside: a loop junction, folded
		{"TestCountBigMixed", "loops.CountBigWorkflow", "J1.iterate J2.false J1.retry J1.exit|completed"},
	},
	// Both sides of every race.
	"waits": {
		{"TestRaceSignalWins", "waits.RaceWorkflow", `J1.signal "answer"|completed`},
		{"TestRaceTimerWins", "waits.RaceWorkflow", "J1.timeout|completed"},
		{"TestPendingSignalWaiting", "waits.PendingWorkflow", `J1.signal "note"|completed`},
		{"TestPendingNothing", "waits.PendingWorkflow", "J1.default|completed"},
		// the activity's error check inside its callback is J2
		{"TestLookupActivityWins", "waits.LookupWorkflow", "J1.activity Lookup J2.success|completed"},
		// the activity is mocked to take 2 hours against a 1-hour timer
		{"TestLookupTimerWins", "waits.LookupWorkflow", "J1.timeout|completed"},
		// signal, nothing, signal: three rounds folded onto the last one
		{"TestCollectThreeRounds", "waits.CollectWorkflow", `J2.iterate J1.signal "item" J2.retry J2.exit|completed`},
		{"TestApproveArrives", "waits.ApproveWorkflow", "J1.success J2.signaled|completed"},
		{"TestApproveTimesOut", "waits.ApproveWorkflow", "J1.success J2.timeout|completed"},
		{"TestReadArrives", "waits.ReadWorkflow", "J1.received|completed"},
		{"TestReadTimesOut", "waits.ReadWorkflow", "J1.not received|completed"},
		{"TestPeekFindsSignal", "waits.PeekWorkflow", "J1.received|completed"},
		{"TestPeekFindsNothing", "waits.PeekWorkflow", "J1.not received|completed"},
	},
	// The compensation defer is a note on the paths past it, never a branch.
	"saga": {
		// fails before the defer: no note (and nothing compensated)
		{"TestBookTripFull", "saga.BookTripWorkflow", "J1.failure|failed"},
		// fails after the defer: the note; the compensation really runs
		{"TestBookTripPaymentFails", "saga.BookTripWorkflow", "J1.success J2.failure|failed [compensation (defer)]"},
		// succeeds: still the note (the defer is registered), not a branch
		{"TestBookTripSucceeds", "saga.BookTripWorkflow", "J1.success J2.success|completed [compensation (defer)]"},
	},
}

// Each fixture test, run alone under "pathkit test", must leave exactly one
// complete trace that lands on its stated path.
func TestLiveFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on the fixtures; skipped with -short")
	}
	fixtures := abs(t, "../../testdata/fixtures")
	t.Chdir(t.TempDir())
	for pkg, cases := range liveFixtures {
		dir := filepath.Join(fixtures, pkg)
		now := graphs(t, dir)
		for _, c := range cases {
			traces := t.TempDir()
			stdout, stderr, code := pathkit(t, "test", dir, "--traces", traces, "--", "-run", "^"+c.test+"$")
			if code != 0 {
				t.Fatalf("pathkit test (%s) exit %d\nstdout:\n%s\nstderr:\n%s", c.test, code, stdout, stderr)
			}
			files := readTraces(t, traces)
			if len(files) != 1 || files[0].Workflow != c.workflow {
				t.Fatalf("%s: want exactly one trace for %s, got %+v", c.test, c.workflow, files)
			}
			out := trace.Check(files[0], now[c.workflow].graph, now[c.workflow].hash)
			if got := withNote(out.Path.Key(), out.Path.Compensation); out.Kind != trace.Matched || got != c.want {
				t.Errorf("%s: trace %v → %s %q (%s); want %q", c.test, files[0].Steps, out.Kind, got, out.Reason, c.want)
				continue
			}
			t.Logf("%s → %s ✓", c.test, c.want)
		}
	}
}
