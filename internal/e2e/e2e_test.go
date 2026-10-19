// Package e2e runs "pathkit test" for real (go test with the overlay) on
// the pilot project and checks each test's recorded trace lands on the
// path the hand-written answer key (testdata/pilot/EXPECTED.md) names.
//
// RULE: EXPECTED.md is never edited to make these tests pass. If PathKit
// and the key disagree and the key looks wrong, stop and ask.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
			out[wf.Name] = current{g, g.CodeHash()}
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
		t.Skip("runs go test on the pilot; skipped with -short (CI runs it)")
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
		now[name] = current{g, g.CodeHash()}
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
		t.Skip("runs go test; skipped with -short (CI runs it)")
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
		t.Skip("runs go test; skipped with -short (CI runs it)")
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
	// must NOT count as complete (a known trap).
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
	// A context parameter written as "_" is named in the marked-up copy.
	"unnamedctx": {
		{"TestSignPositive", "unnamedctx.Sign", "J1.true|completed"},
		{"TestSignNegative", "unnamedctx.Sign", "J1.false J2.true|completed"},
		{"TestSignZero", "unnamedctx.Sign", "J1.false J2.false|completed"},
		{"TestStrictEmptyName", "unnamedctx.Strict", "J1.true|failed"},
		{"TestStrictNamed", "unnamedctx.Strict", "J1.false|completed"},
	},
	// Another handle of the same named signal channel can be closed.
	"closedchannelleak": {
		{"TestSameNameClosedReachesTheClosedSide", "closedchannelleak.SameNameClosed", "J1.true|completed"},
		{"TestInlineCloseReachesTheClosedSide", "closedchannelleak.InlineClose", "J1.true|completed"},
		{"TestHelperClosedReachesTheClosedSide", "closedchannelleak.HelperClosed", "J1.true|completed"},
	},
	// A channel the workflow closes itself really reaches "more == false".
	"closedchannel": {
		{"TestOwnClosedReachesTheClosedSide", "closedchannel.OwnClosed", "J1.true|completed"},
		{"TestLateOwnClosedReachesTheClosedSide", "closedchannel.LateOwnClosed", "J1.true|completed"},
		{"TestLateAssignedTwiceReachesTheClosedSide", "closedchannel.LateAssignedTwice", "J1.true|completed"},
	},
	// Near-misses of the list-built-from-list detector, run for real.
	"builtlist": {
		{"TestConditionalAppendAllSkipped", "builtlist.ConditionalAppend", "J1.iterate J2.false J1.retry J1.exit J3.exit|completed"},
		{"TestContinueBeforeAppendAllSkipped", "builtlist.ContinueBeforeAppend", "J1.iterate J2.true J1.retry J1.exit J3.exit|completed"},
		{"TestStartsNotEmptyFirstLoopSkipped", "builtlist.StartsNotEmpty", "J1.exit J2.iterate J2.retry J2.exit|completed"},
		{"TestAppendedBetweenFirstLoopSkipped", "builtlist.AppendedBetween", "J1.exit J2.iterate J2.retry J2.exit|completed"},
		{"TestReplacedBetweenFirstLoopRan", "builtlist.ReplacedBetween", "J1.iterate J1.retry J1.exit J2.true J3.iterate J3.retry J3.exit|completed"},
		{"TestClosureAppendsFirstLoopSkipped", "builtlist.ClosureAppends", "J1.exit J2.true J3.iterate J3.retry J3.exit|completed"},
		{"TestReturnBeforeAppendFirstLoopRan", "builtlist.ReturnBeforeAppend", "J1.iterate J2.true|completed"},
		{"TestMethodResetFirstLoopRanSecondSkipped", "builtlist.MethodReset", "J1.iterate J1.retry J1.exit J2.true J3.exit|completed"},
		{"TestMethodValueResetFirstLoopRanSecondSkipped", "builtlist.MethodValueReset", "J1.iterate J1.retry J1.exit J2.true J3.exit|completed"},
		{"TestParenAddressFirstLoopRanSecondSkipped", "builtlist.ParenAddress", "J1.iterate J1.retry J1.exit J2.true J3.exit|completed"},
	},
	// Near-misses of the loop-exit-before-first-trip detector, run for real:
	// each reaches "left the loop before its first trip".
	"loopfirst": {
		{"TestSetBeforeSkipsTheLoop", "loopfirst.SetBefore", "J1.true J2.exit|completed"},
		{"TestClosureSetsBeforeSkipsTheLoop", "loopfirst.ClosureSetsBefore", "J1.true J2.exit|completed"},
		{"TestAddressBeforeSkipsTheLoop", "loopfirst.AddressBefore", "J1.true J2.exit|completed"},
		{"TestFromParameterSkipsTheLoop", "loopfirst.FromParameter", "J1.exit|completed"},
		{"TestDeclaredOutsideOuterLoopSecondTrip", "loopfirst.DeclaredOutsideOuterLoop", "J1.iterate J2.exit J1.retry J1.exit|completed"},
		{"TestAssignedBeforeSkipsTheLoop", "loopfirst.AssignedBefore", "J1.exit|completed"},
		{"TestPackageLevelSecondRun", "loopfirst.PackageLevel", "J1.exit|completed"},
		{"TestParenAssignSkipsTheLoop", "loopfirst.ParenAssign", "J1.true J2.exit|completed"},
	},
	// Near-misses of the contradictory-get-result detector, run for real:
	// the first test sees Ok == true, the second then sees Ok == false.
	"getvalue": {
		{"TestSecondGetChangesIt", "getvalue.SecondGet", "J1.true J2.true|completed"},
		{"TestWriteBetweenChangesIt", "getvalue.WriteBetween", "J1.true J2.true|completed"},
		{"TestAddressPassedOnChangesIt", "getvalue.AddressPassedOn", "J1.true J2.true|completed"},
		{"TestMethodOnResultChangesIt", "getvalue.MethodOnResult", "J1.true J2.true|completed"},
		{"TestMethodValueOnResultChangesIt", "getvalue.MethodValueOnResult", "J1.true J2.true|completed"},
		{"TestPointerAliasChangesIt", "getvalue.PointerAlias", "J1.true J2.true|completed"},
		{"TestPointerPassedChangesIt", "getvalue.PointerPassed", "J1.true J2.true|completed"},
		{"TestClosureWritesChangesIt", "getvalue.ClosureWrites", "J1.true J2.true|completed"},
		{"TestPointerToPointerChangesIt", "getvalue.PointerToPointerPassed", "J1.true J2.true|completed"},
		{"TestParenAliasChangesIt", "getvalue.ParenAlias", "J1.true J2.true|completed"},
	},
	// Near-misses of the data-flow detectors, run for real: these paths are
	// reachable, so they must stay testable.
	"dataflow": {
		{"TestReassignedBothTrue", "dataflow.Reassigned", "J1.true J2.true|completed"},
		{"TestListAppendedSecondLoopRuns", "dataflow.ListAppended", "J1.exit J2.iterate J2.retry J2.exit|completed"},
		{"TestDifferentListsOneEmpty", "dataflow.DifferentLists", "J1.exit J2.iterate J2.retry J2.exit|completed"},
		{"TestLongStringsOnlyFirstMatches", "dataflow.LongStrings", "J1.true J2.false|completed"},
		{"TestMapLengthFilledThenNotEmpty", "dataflow.MapLength", "J1.true J2.false|completed"},
		{"TestZeroThenStoreBothFalse", "dataflow.ZeroThenStore", "J1.false J2.false|completed"},
	},
	// Both special-test detectors: the SDK's own setters reach the branch.
	"versiongate": {
		{"TestOldBranchWithOnGetVersion", "versiongate.CatchEqDefault", "J1.true|completed"},
		{"TestNewBranchByDefault", "versiongate.CatchEqDefault", "J1.false|"},
	},
	"historysuggest": {
		{"TestSuggestedWithSetter", "historysuggest.CatchDirect", "J1.true|continued-as-new"},
		{"TestNotSuggestedByDefault", "historysuggest.CatchDirect", "J1.false|"},
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
		t.Skip("runs go test on the fixtures; skipped with -short (CI runs it)")
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

// The scope example: excluding ShipmentWorkflow (with a
// reason) leaves 29 paths, of which the pilot's tests cover 17: 58.6%.
// The numbers come from real runs (analyze, and one pathkit test of the
// whole pilot), and must agree with EXPECTED.md, which is never edited.
func TestScopeExcludesShipment(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on the pilot; skipped with -short (CI runs it)")
	}
	pilot := abs(t, "../../testdata/pilot")
	cfg := abs(t, "../../testdata/scopes/no-shipment/.pathkitrc.json")
	key, err := expected.Read(filepath.Join(pilot, "EXPECTED.md"))
	if err != nil {
		t.Fatal(err)
	}
	const excluded = "shipment.ShipmentWorkflow"
	wantPaths, wantCovered := 0, 0
	for name, w := range key {
		if name != excluded {
			wantPaths += w.Count
			wantCovered += w.Covered
		}
	}
	if wantPaths != 29 || wantCovered != 17 {
		t.Fatalf("EXPECTED.md without shipment: %d paths, %d covered; SETUP-GUIDE.md section 8 says 29 and 17", wantPaths, wantCovered)
	}
	t.Chdir(t.TempDir())

	// 1. analyze: 29 paths, shipment listed with its reason.
	stdout, stderr, code := pathkit(t, "analyze", pilot+"/...", "--summary", "--config", cfg)
	if code != 0 || stderr != "" {
		t.Fatalf("analyze: exit %d\n%s", code, stderr)
	}
	paths := 0
	for _, line := range strings.Split(stdout, "\n") {
		if n, ok := strings.CutPrefix(line, "Total paths: "); ok {
			v, _ := strconv.Atoi(n)
			paths += v
		}
	}
	if paths != 29 || !strings.Contains(stdout, "  shipment.ShipmentWorkflow: needs a real carrier sandbox\n") {
		t.Errorf("analyze: %d paths (want 29), excluded list:\n%s", paths, stdout)
	}

	// 2. One pathkit test of the whole pilot (the folder comes from the
	// config's "packages"): every test runs, shipment's are not recorded.
	traces := t.TempDir()
	stdout, stderr, code = pathkit(t, "test", "--config", cfg, "--traces", traces)
	if code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "pathkit test: not recording shipment.ShipmentWorkflow: excluded from scope (needs a real carrier sandbox)\n") ||
		!strings.Contains(stdout, "ok  \texample.com/pilot/shipment") {
		t.Errorf("shipment's tests must run but not be recorded:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	// 3. Count the distinct covered paths.
	res, err := load.Load(pilot + "/...")
	if err != nil {
		t.Fatal(err)
	}
	workflows := map[string]discover.Workflow{}
	for _, wf := range discover.Find(res.Packages) {
		workflows[wf.Name] = wf
	}
	covered := map[string]bool{}
	for _, f := range readTraces(t, traces) {
		if f.Workflow == excluded {
			t.Fatalf("a trace was recorded for the excluded %s", excluded)
		}
		g, err := expected.Build(workflows, f.Workflow)
		if err != nil {
			t.Fatal(err)
		}
		out := trace.Check(f, g, g.CodeHash())
		if out.Kind != trace.Matched {
			t.Fatalf("%s trace %v: %s (%s)", f.Workflow, f.Steps, out.Kind, out.Reason)
		}
		covered[f.Workflow+" "+out.Path.Key()] = true
	}
	pct := fmt.Sprintf("%.1f%%", float64(len(covered))/float64(paths)*100)
	if len(covered) != 17 || pct != "58.6%" {
		t.Errorf("covered %d of %d paths = %s; want 17 of 29 = 58.6%%", len(covered), paths, pct)
	}
	t.Logf("scope without shipment: %d paths, %d covered = %s", paths, len(covered), pct)
}

// A workflow only "include" added (unexported lowerFlow) is recorded and
// matched like any other.
func TestAddedByConfigRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test; skipped with -short (CI runs it)")
	}
	cfg := abs(t, "../../testdata/scopes/added/.pathkitrc.json")
	dir := abs(t, "../../testdata/fixtures/scope")
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	stdout, stderr, code := pathkit(t, "test", dir, "--config", cfg, "--traces", traces)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	files := readTraces(t, traces)
	if len(files) != 1 || files[0].Workflow != "scope.lowerFlow" || files[0].Status != "complete" {
		t.Fatalf("want one complete trace for scope.lowerFlow, got %+v", files)
	}
	stdout, _, code = pathkit(t, "traces", dir, "--config", cfg, "--traces", traces)
	if code != 0 || !strings.Contains(stdout, "scope.lowerFlow: path 1 (J1.true → End (completed))\n") {
		t.Errorf("traces: exit %d\n%s", code, stdout)
	}
}

// covJSON is the part of "pathkit coverage --json" these tests read.
type covJSON struct {
	Workflows []struct {
		Name     string
		Paths    struct{ Total, Covered int }
		PathList []struct {
			Steps        []string
			End          string
			Compensation bool
			Covered      bool
		}
	}
	Excluded []struct{ Name, Reason string }
	Total    struct {
		Total, Covered int
		Percent        float64
	}
}

// pathkit coverage on real recorded runs gives exactly what EXPECTED.md
// predicts: 20 of 38 (52.6%) for the whole pilot, 17 of 29 (58.6%) with
// the no-shipment scope, and for every workflow the very paths the key's
// tests point to.
// TestCoverageMatchesKey also runs with -short: it records the whole
// pilot once (a few seconds), so a quick local run still has one real
// end-to-end check against EXPECTED.md.
func TestCoverageMatchesKey(t *testing.T) {
	pilot := abs(t, "../../testdata/pilot")
	goldenDir := abs(t, "../../testdata/golden/pilot") // before any t.Chdir
	cfg := abs(t, "../../testdata/scopes/no-shipment/.pathkitrc.json")
	key, err := expected.Read(filepath.Join(pilot, "EXPECTED.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	traces := t.TempDir()
	// pathkit test never deletes a
	// report-history.json: not next to the default page, not in the
	// trace folder it clears, not even in its own overlay folder.
	histories := []string{
		filepath.Join(".pathkit", "report-history.json"),
		filepath.Join(".pathkit", "overlay", "report-history.json"),
		filepath.Join(traces, "report-history.json"),
	}
	for _, h := range histories {
		if err := os.MkdirAll(filepath.Dir(h), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(h, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if stdout, stderr, code := pathkit(t, "test", pilot+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, h := range histories {
		if data, err := os.ReadFile(h); err != nil || string(data) != "keep me" {
			t.Errorf("pathkit test deleted or changed %s: %v", h, err)
		}
	}

	check := func(t *testing.T, args []string, wantCovered, wantPaths int, wantPct float64, skip string) {
		stdout, stderr, code := pathkit(t, append([]string{"coverage", pilot + "/...", "--traces", traces, "--json"}, args...)...)
		if code != 0 || stderr != "" {
			t.Fatalf("coverage: exit %d\n%s", code, stderr)
		}
		var got covJSON
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatal(err)
		}
		if got.Total.Covered != wantCovered || got.Total.Total != wantPaths || got.Total.Percent != wantPct {
			t.Errorf("total %d/%d = %v%%, want %d/%d = %v%%", got.Total.Covered, got.Total.Total, got.Total.Percent, wantCovered, wantPaths, wantPct)
		}
		for _, w := range got.Workflows {
			ew := key[w.Name]
			if ew == nil || w.Name == skip {
				t.Errorf("%s is in the coverage but shouldn't be", w.Name)
				continue
			}
			// The set of covered paths must be exactly the paths the key's
			// tests point to (compared as keys, with the note).
			var gotKeys, wantKeys []string
			for _, p := range w.PathList {
				if p.Covered {
					k := strings.Join(p.Steps, " ") + "|" + p.End
					gotKeys = append(gotKeys, withNote(k, p.Compensation))
				}
			}
			seen := map[int]bool{}
			for _, tst := range ew.Tests {
				if !seen[tst.Path] {
					seen[tst.Path] = true
					p := ew.Paths[tst.Path-1]
					wantKeys = append(wantKeys, withNote(p.Key, p.Compensation))
				}
			}
			slices.Sort(gotKeys)
			slices.Sort(wantKeys)
			if w.Paths.Covered != ew.Covered || w.Paths.Total != ew.Count || !slices.Equal(gotKeys, wantKeys) {
				t.Errorf("%s: %d/%d covered %v; EXPECTED.md: %d/%d covered %v", w.Name, w.Paths.Covered, w.Paths.Total, gotKeys, ew.Covered, ew.Count, wantKeys)
			}
		}
	}
	t.Run("whole pilot", func(t *testing.T) { check(t, nil, 20, 38, 52.6, "") })
	t.Run("no-shipment scope", func(t *testing.T) {
		check(t, []string{"--config", cfg}, 17, 29, 58.6, "shipment.ShipmentWorkflow")
	})

	// The text report's total line, as the setup guide shows it.
	stdout, _, _ := pathkit(t, "coverage", pilot+"/...", "--traces", traces)
	if !strings.Contains(stdout, "\n38 paths total · 20 covered · 18 missed · 52.6% coverage\n") {
		t.Errorf("text total line missing:\n%s", stdout)
	}

	// report and coverage agree field by field, because they share one
	// code path (measure). report --json is coverage --json plus "file" and
	// "priority" on each workflow and "excludedBy"; with those three
	// removed, the two must be identical, unscoped and scoped.
	agree := func(t *testing.T, args []string, wantTotal string, wantPriority map[string]string) {
		cov, covErr, covCode := pathkit(t, append([]string{"coverage", pilot + "/...", "--traces", traces, "--json"}, args...)...)
		rep, repErr, repCode := pathkit(t, append([]string{"report", pilot + "/...", "--traces", traces, "--json"}, args...)...)
		if covCode != 0 || repCode != 0 || covErr != "" || repErr != "" {
			t.Fatalf("coverage exit %d %q, report exit %d %q", covCode, covErr, repCode, repErr)
		}
		var c, r map[string]any
		if err := json.Unmarshal([]byte(cov), &c); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(rep), &r); err != nil {
			t.Fatal(err)
		}
		gotPriority := map[string]string{}
		delete(r, "excludedBy")
		for _, w := range r["workflows"].([]any) {
			w := w.(map[string]any)
			if f, _ := w["file"].(string); !strings.HasSuffix(f, ".go") {
				t.Errorf("%v: file = %q", w["name"], f)
			}
			gotPriority[w["name"].(string)], _ = w["priority"].(string)
			delete(w, "file")
			delete(w, "priority")
		}
		if !reflect.DeepEqual(c, r) {
			t.Errorf("report --json (minus file, priority, excludedBy) differs from coverage --json\ncoverage:\n%s\nreport:\n%s", cov, rep)
		}
		// 8 fields as before, plus the 3 added with path classification
		// (classification, coverageBasis, classificationShown), unread, and the
		// handler, call outcome and loop round tables.
		if len(c) != 15 || len(c["workflows"].([]any)) != len(wantPriority) {
			t.Errorf("compared %d top-level fields and %d workflows; want 15 and %d", len(c), len(c["workflows"].([]any)), len(wantPriority))
		}
		if !maps.Equal(gotPriority, wantPriority) {
			t.Errorf("priorities %v, want %v", gotPriority, wantPriority)
		}
		text, _, _ := pathkit(t, append([]string{"report", pilot + "/...", "--traces", traces}, args...)...)
		if !strings.Contains(text, "\n"+wantTotal+"\n") || !strings.Contains(text, "\nTraces: 21 read · ") {
			t.Errorf("report text lacks %q or the Traces line:\n%s", wantTotal, text)
		}

		// The HTML page shows exactly report's numbers.
		out := filepath.Join(t.TempDir(), "report.html")
		if _, stderr, code := pathkit(t, append([]string{"report", pilot + "/...", "--traces", traces, "--html=" + out}, args...)...); code != 0 {
			t.Fatalf("report --html: exit %d %s", code, stderr)
		}
		page, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		htmlMatchesReport(t, string(page), rep, wantTotal)
	}
	// D11 from EXPECTED.md's numbers: 33.3% High; 50%, 60%, 66.7% Medium; 100% Low.
	priorities := map[string]string{
		"approval.ApprovalWorkflow": "Medium", "billing.SubscriptionWorkflow": "Medium",
		"fulfillment.OrderFulfillmentWorkflow": "Medium", "fulfillment.PaymentWorkflow": "Medium",
		"orders.OrderWorkflow": "Low", "polling.ReportPollingWorkflow": "Medium",
		"reports.DailyReportWorkflow": "Medium", "shipment.ShipmentWorkflow": "High",
	}
	t.Run("report agrees with coverage, whole pilot", func(t *testing.T) {
		agree(t, nil, "38 paths total · 20 covered · 18 missed · 52.6% project coverage", priorities)
	})
	t.Run("report agrees with coverage, no-shipment scope", func(t *testing.T) {
		scoped := maps.Clone(priorities)
		delete(scoped, "shipment.ShipmentWorkflow")
		agree(t, []string{"--config", cfg}, "29 paths total · 17 covered · 12 missed · 58.6% project coverage", scoped)
	})

	// Path classification on the real pilot, run from the
	// pilot's own folder (so file names print as they do for a user):
	//   - with classification off, the text is byte for byte what the
	//     program printed BEFORE classification existed
	//     (testdata/golden/pilot/baseline-report.txt), and the JSON minus
	//     the new fields is the old JSON;
	//   - with --coverage-basis, the classified output is pinned too;
	//   - the numbers are the answer key's: 38 paths, 20 covered, 52.6%,
	//     branches 39/50, and every path testable.
	t.Run("pilot golden, classification off and on", func(t *testing.T) {
		read := func(name string) string {
			data, err := os.ReadFile(filepath.Join(goldenDir, name))
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
		t.Chdir(pilot)
		text, stderr, code := pathkit(t, "report", "./...", "--traces", traces)
		update := func(name, content string) {
			// Regenerate on purpose: PATHKIT_UPDATE_GOLDEN=1 go test ./internal/e2e -run TestCoverageMatchesKey
			if os.Getenv("PATHKIT_UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(filepath.Join(goldenDir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		update("baseline-report.txt", slashHeaders(text))
		if code != 0 || stderr != "" || slashHeaders(text) != read("baseline-report.txt") {
			t.Errorf("report text (classification off) differs from baseline-report.txt: exit %d %q", code, stderr)
		}
		js, _, _ := pathkit(t, "report", "./...", "--traces", traces, "--json")
		var got, base map[string]any
		if err := json.Unmarshal([]byte(js), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(read("baseline-report.json")), &base); err != nil {
			t.Fatal(err)
		}
		stripClassification(got)
		if !reflect.DeepEqual(got, base) {
			t.Errorf("report --json minus the new fields differs from baseline-report.json")
		}

		text, stderr, code = pathkit(t, "report", "./...", "--traces", traces, "--coverage-basis", "testable")
		update("classified-report.txt", slashHeaders(text))
		if code != 0 || stderr != "" || slashHeaders(text) != read("classified-report.txt") {
			t.Errorf("classified report text differs from classified-report.txt: exit %d %q\n%s", code, stderr, text)
		}
		js, _, _ = pathkit(t, "report", "./...", "--traces", traces, "--json", "--coverage-basis", "testable")
		update("classified-report.json", js)
		if js != read("classified-report.json") {
			t.Error("classified report --json differs from classified-report.json")
		}
		var cj covJSON
		if err := json.Unmarshal([]byte(js), &cj); err != nil {
			t.Fatal(err)
		}
		if cj.Total.Total != 38 || cj.Total.Covered != 20 || cj.Total.Percent != 52.6 {
			t.Errorf("total %+v, want 38 paths, 20 covered, 52.6%%", cj.Total)
		}
		for _, w := range []string{"39/50 (78.0%)", "Testable:            38", "Cannot be tested:    0", "Needs special test:  0", "Needs review:        0"} {
			if !strings.Contains(text, w) {
				t.Errorf("classified text lacks %q", w)
			}
		}
	})

	// D5: the page numbers every path exactly as analyze
	// does (report.html's and analysis.html's alike).
	t.Run("same path numbers as analyze", func(t *testing.T) {
		text, _, code := pathkit(t, "analyze", pilot+"/...")
		if code != 0 {
			t.Fatal("analyze failed")
		}
		want := analyzeListing(text)
		r, a := filepath.Join(t.TempDir(), "r.html"), filepath.Join(t.TempDir(), "a.html")
		pathkit(t, "report", pilot+"/...", "--traces", traces, "--html="+r)
		pathkit(t, "analyze", pilot+"/...", "--html="+a)
		for _, file := range []string{r, a} {
			page, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			got := analysisTab(t, string(page))
			if len(got) != 8 || !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the page differs from analyze\n got %v\nwant %v", filepath.Base(file), got, want)
			}
		}
	})
}

// analyzeListing reads analyze's text output: workflow -> its numbered
// path lines ("1. Start -> ...").
func analyzeListing(text string) map[string][]string {
	out := map[string][]string{}
	name := ""
	for _, line := range strings.Split(text, "\n") {
		if n, ok := strings.CutPrefix(line, "Workflow: "); ok {
			name = n
		} else if m := regexp.MustCompile(`^  (\d+\. .*)$`).FindStringSubmatch(line); m != nil && name != "" {
			out[name] = append(out[name], m[1])
		}
	}
	return out
}

var (
	// A workflow's block in the page: the numbers the page puts on it.
	workflowBlockRe = regexp.MustCompile(`<details class="wf" id="wf-\d+"(?: open)? data-workflow="([^"]*)" data-covered="(\d+)" data-paths="(\d+)" data-priority="([^"]*)" data-truncated="(true|false)">`)
	dataBlockRe     = regexp.MustCompile(`(?s)<script id="pk-data" type="application/json">(.*?)</script>`)
	attrRe          = func(name string) *regexp.Regexp { return regexp.MustCompile(` ` + name + `="([^"]*)"`) }
)

// pageRow is one path as the page's data block has it.
type pageRow struct {
	Number  int
	ID      string
	Covered bool
	Bucket  string
	Label   string
	By      string
	Text    string
}

type pageWorkflow struct {
	Name, File string
	Covered    int
	Total      int
	Rows       []pageRow
}

// pageWorkflows reads the page's data block (the paths are not written as
// HTML) into plain values.
func pageWorkflows(t *testing.T, page string) []pageWorkflow {
	t.Helper()
	m := dataBlockRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the page has no data block")
	}
	var d struct {
		S []string `json:"s"`
		W []struct {
			N string  `json:"n"`
			F string  `json:"f"`
			C int     `json:"c"`
			T int     `json:"t"`
			R [][]any `json:"r"`
		} `json:"w"`
	}
	if err := json.Unmarshal([]byte(m[1]), &d); err != nil {
		t.Fatal(err)
	}
	str := func(v any) string {
		i := int(v.(float64))
		if i < 0 {
			return ""
		}
		return d.S[i]
	}
	var out []pageWorkflow
	for _, w := range d.W {
		pw := pageWorkflow{Name: w.N, File: w.F, Covered: w.C, Total: w.T}
		for _, r := range w.R {
			var parts []string
			for _, v := range r[7].([]any) {
				parts = append(parts, str(v))
			}
			pw.Rows = append(pw.Rows, pageRow{Number: int(r[0].(float64)), ID: r[1].(string), Covered: r[2].(float64) != 0,
				Bucket: str(r[3]), Label: str(r[4]), By: str(r[5]), Text: strings.Join(parts, "--> ")})
		}
		out = append(out, pw)
	}
	return out
}

// analysisTab reads a page's paths: workflow -> "N. <path>" lines.
func analysisTab(t *testing.T, page string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, w := range pageWorkflows(t, page) {
		for _, r := range w.Rows {
			out[w.Name] = append(out[w.Name], strconv.Itoa(r.Number)+". "+r.Text)
		}
	}
	return out
}

// htmlMatchesReport requires the page's numbers, read from its data-*
// attributes and visible text, to equal report --json field by field.
func htmlMatchesReport(t *testing.T, page, reportJSON, wantTotal string) {
	t.Helper()
	var rep struct {
		Workflows []struct {
			Name     string
			File     string
			Priority *string
			Paths    struct{ Total, Covered int }
			PathList []struct {
				Number  int
				Text    string
				Covered bool
			}
		}
		Excluded []struct{ Name, Reason string }
		Total    struct{ Total, Covered int }
		Traces   map[string]int
	}
	if err := json.Unmarshal([]byte(reportJSON), &rep); err != nil {
		t.Fatal(err)
	}
	attr := func(src, name string) string {
		m := attrRe(name).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("no %s attribute", name)
		}
		return html.UnescapeString(m[1])
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }

	if num(attr(page, "data-total-paths")) != rep.Total.Total || num(attr(page, "data-total-covered")) != rep.Total.Covered {
		t.Errorf("total: html %s/%s, json %d/%d", attr(page, "data-total-covered"), attr(page, "data-total-paths"), rep.Total.Covered, rep.Total.Total)
	}
	if !strings.Contains(html.UnescapeString(page), ">"+wantTotal+"</p>") {
		t.Errorf("the visible total line is not %q", wantTotal)
	}

	cards := workflowBlockRe.FindAllStringSubmatch(page, -1)
	data := pageWorkflows(t, page)
	if len(cards) != len(rep.Workflows) || len(data) != len(rep.Workflows) {
		t.Fatalf("%d workflow blocks and %d in the data, %d in the JSON", len(cards), len(data), len(rep.Workflows))
	}
	for i, c := range cards {
		w := rep.Workflows[i]
		prio := ""
		if w.Priority != nil {
			prio = *w.Priority
		}
		if html.UnescapeString(c[1]) != w.Name || num(c[2]) != w.Paths.Covered || num(c[3]) != w.Paths.Total || c[4] != prio {
			t.Errorf("workflow %d: html %s %s/%s %s; json %s %d/%d %s", i, c[1], c[2], c[3], c[4], w.Name, w.Paths.Covered, w.Paths.Total, prio)
		}
		d := data[i]
		if d.Name != w.Name || d.File != w.File || d.Covered != w.Paths.Covered || d.Total != w.Paths.Total {
			t.Errorf("%s: data block %s %s %d/%d; json %s %d/%d", w.Name, d.Name, d.File, d.Covered, d.Total, w.File, w.Paths.Covered, w.Paths.Total)
		}
		if !strings.Contains(page, `<p class="file">`+w.File+`</p>`) {
			t.Errorf("%s: file %s not shown", w.Name, w.File)
		}
		if len(d.Rows) != len(w.PathList) {
			t.Fatalf("%s: %d paths in the page, %d in the JSON", w.Name, len(d.Rows), len(w.PathList))
		}
		for j, r := range d.Rows {
			p := w.PathList[j]
			if r.Number != p.Number || r.Covered != p.Covered || r.Text != p.Text {
				t.Errorf("%s path %d: html %d covered=%v %q; json %d covered=%v %q", w.Name, j+1, r.Number, r.Covered, r.Text, p.Number, p.Covered, p.Text)
			}
		}
	}

	var excluded []string
	for _, m := range regexp.MustCompile(`<li data-excluded-workflow="([^"]*)"><code>[^<]*</code>: <span class="reason">([^<]*)</span>`).FindAllStringSubmatch(page, -1) {
		excluded = append(excluded, html.UnescapeString(m[1])+": "+html.UnescapeString(m[2]))
	}
	var wantExcluded []string
	for _, e := range rep.Excluded {
		wantExcluded = append(wantExcluded, e.Name+": "+e.Reason)
	}
	if !slices.Equal(excluded, wantExcluded) {
		t.Errorf("excluded: html %v, json %v", excluded, wantExcluded)
	}

	for jsonKey, attrName := range map[string]string{"read": "data-read", "counted": "data-counted", "unmatched": "data-unmatched",
		"stale": "data-stale", "incomplete": "data-incomplete", "excluded": "data-excluded-traces", "unknownWorkflow": "data-unknown",
		"unreadable": "data-unreadable", "otherWorkflows": "data-other"} {
		if got := num(attr(page, attrName)); got != rep.Traces[jsonKey] {
			t.Errorf("traces %s: html %d, json %d", jsonKey, got, rep.Traces[jsonKey])
		}
	}
	if !strings.Contains(page, ">Traces: 21 read · ") {
		t.Errorf("the visible Traces line is missing")
	}
}

// pathkit prepare writes the overlay and prints the go test command; that
// command, run by hand (not through pathkit), records traces. A plain
// "go test" records nothing.
func TestPrepare(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test; skipped with -short (CI runs it)")
	}
	orders := abs(t, "../../testdata/pilot/orders")
	t.Chdir(t.TempDir())
	traces := abs(t, "traces")
	stdout, stderr, code := pathkit(t, "prepare", orders, "--traces", traces)
	if code != 0 {
		t.Fatalf("prepare: exit %d\n%s", code, stderr)
	}
	line := strings.TrimSpace(stdout)
	if !strings.HasPrefix(line, "go test -overlay=") || strings.Count(stdout, "\n") != 1 ||
		!strings.Contains(stderr, "pathkit prepare: run it in "+orders+"\n") {
		t.Fatalf("stdout %q\nstderr %q", stdout, stderr)
	}

	goTest := func(args ...string) {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = orders
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	goTest("test", "-count=1", ".") // plain go test: records nothing
	if n := len(readTraces(t, traces)); n != 0 {
		t.Fatalf("plain go test wrote %d traces, want 0", n)
	}
	goTest(strings.Fields(line)[1:]...) // exactly the printed command
	files := readTraces(t, traces)
	if len(files) != 3 {
		t.Fatalf("the printed command wrote %d traces, want 3 (the orders tests)", len(files))
	}
	stdout, _, code = pathkit(t, "coverage", orders, "--traces", traces)
	if code != 0 || !strings.Contains(stdout, "Covered: 3/3 (100.0%)") {
		t.Errorf("coverage of the prepared run: exit %d\n%s", code, stdout)
	}
}

// CI must run every test: -short is for quick local runs only.
// This fails if a "go test" line in the CI workflow ever gets -short.
func TestCIRunsEverything(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "go test") {
			continue
		}
		runs++
		if strings.Contains(line, "-short") {
			t.Errorf("ci.yml line %d uses -short, so CI would skip the slow tests: %s", i+1, strings.TrimSpace(line))
		}
	}
	if runs == 0 {
		t.Error("ci.yml has no go test line")
	}
}

// stripClassification removes the fields added with path classification,
// so what is left can be compared with output from before it existed.
func stripClassification(v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"stableId", "bucket", "label", "source", "confidence", "possibleInProduction", "explanation", "ruleId"} {
			delete(x, k)
		}
		if _, ok := x["pathList"]; ok {
			delete(x, "classification")
		}
		if _, ok := x["workflows"]; ok {
			delete(x, "classification")
			delete(x, "coverageBasis")
			delete(x, "classificationShown")
			delete(x, "unread")                                                    // the "could not fully read" panel (M7.8)
			for _, k := range []string{"callOutcomes", "handlers", "loopRounds"} { // the outcome, handler and loop tables (M8)
				delete(x, k)
			}
		}
		if _, ok := x["threshold"]; ok {
			delete(x, "basis")
		}
		for _, c := range x {
			stripClassification(c)
		}
	case []any:
		for _, c := range x {
			stripClassification(c)
		}
	}
}

// workflowHeader matches the line that starts each workflow's block in the
// text report: "pkg.Workflow (folder/file.go)".
var workflowHeader = regexp.MustCompile(`(?m)^([A-Za-z_][\w.]*\.\w+ \()([^)\n]*)\)$`)

// slashHeaders writes the file in those header lines with forward slashes.
// The text report shows a file with the system's own separator (documented
// in SETUP-GUIDE.md); the golden files are written with "/", so on Windows
// the actual text is converted before it is compared. Nothing else changes.
func slashHeaders(text string) string {
	return workflowHeader.ReplaceAllStringFunc(text, func(line string) string {
		m := workflowHeader.FindStringSubmatch(line)
		return m[1] + filepath.ToSlash(m[2]) + ")"
	})
}
