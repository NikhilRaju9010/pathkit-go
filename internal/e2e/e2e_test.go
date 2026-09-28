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

// m2Workflows are the workflows M3 can record.
var m2Workflows = []string{"orders.OrderWorkflow", "fulfillment.PaymentWorkflow", "reports.DailyReportWorkflow"}

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

// graphs builds the current graph and hash of every M2 pilot workflow.
func graphs(t *testing.T, pilot string) map[string]current {
	t.Helper()
	res, err := load.Load(pilot + "/...")
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
	now := graphs(t, pilot)
	before := hashTree(t, pilot)
	t.Chdir(t.TempDir()) // pathkit writes .pathkit/ here, never into the pilot

	checked := 0
	for _, name := range m2Workflows {
		ew := key[name]
		if ew == nil || len(ew.Tests) == 0 {
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
			want := ew.Paths[test.Path-1]
			if out.Kind != trace.Matched || out.Path.Key() != want {
				t.Errorf("%s: trace %v → %s %q (%s); EXPECTED.md says path %d = %q",
					test.Name, files[0].Steps, out.Kind, out.Path.Key(), out.Reason, test.Path, want)
				continue
			}
			t.Logf("%s → EXPECTED path %d (%s) ✓", test.Name, test.Path, want)
			checked++
		}
	}
	if checked != 8 {
		t.Errorf("checked %d tests against EXPECTED.md, want 8 (3 orders, 2 payment, 3 daily report)", checked)
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
