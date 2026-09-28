package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func TestTestCommandArgErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"test", "a", "b"}, "pathkit test: expected at most one folder argument, got 2\n"},
		{[]string{"test", pilot + "/orders/orders.go"}, "pathkit test: expected a package folder or folder/..., not a file: " + pilot + "/orders/orders.go\n"},
		{[]string{"test", "nowhere"}, "pathkit test: Directory not found: nowhere\n"},
		{[]string{"traces", "a", "b"}, "pathkit traces: expected at most one folder argument, got 2\n"},
	}
	for _, tt := range tests {
		_, stderr, code := run(t, tt.args...)
		if code != ExitError || stderr != tt.want {
			t.Errorf("%v: code=%d stderr=%q, want %q", tt.args, code, stderr, tt.want)
		}
	}
}

func TestTracesNoTraceFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "traces")
	_, stderr, code := run(t, "traces", pilot+"/orders", "--traces", dir)
	want := "pathkit traces: no trace files found in " + dir +
		"; coverage is recorded only by \"pathkit test\" (plain \"go test\" records nothing)\n"
	if code != ExitError || stderr != want {
		t.Errorf("code=%d stderr=%q\nwant %q", code, stderr, want)
	}
}

// writeTrace writes a hand-made trace file.
func writeTrace(t *testing.T, dir, name string, fields map[string]any) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".trace.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTracesOutcomes(t *testing.T) {
	res, err := load.Load(pilot + "/orders")
	if err != nil {
		t.Fatal(err)
	}
	wf := discover.Find(res.Packages)[0]
	hash := model.FunctionHash(wf.Pkg.Fset, wf.Func)

	dir := t.TempDir()
	base := func(status, hash string, steps ...string) map[string]any {
		return map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
			"functionHash": hash, "status": status, "steps": steps}
	}
	writeTrace(t, dir, "a-matched", base("complete", hash, "J1.false", "J2.failure"))
	writeTrace(t, dir, "b-unmatched", base("complete", hash, "J2.failure"))
	writeTrace(t, dir, "c-stale", base("complete", "0000000000000000", "J1.true"))
	writeTrace(t, dir, "d-incomplete", base("incomplete", hash, "J1.false"))
	writeTrace(t, dir, "e-unknown", map[string]any{"schemaVersion": 1, "workflow": "nope.Nope", "status": "complete", "steps": []string{}})
	if err := os.WriteFile(filepath.Join(dir, "f-broken.trace.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := run(t, "traces", pilot+"/orders", "--traces", dir)
	if code != ExitOK {
		t.Fatalf("code=%d\n%s", code, stdout)
	}
	for _, want := range []string{
		"orders.OrderWorkflow: path 2 (J1.false J2.failure → End (failed))\n",
		"orders.OrderWorkflow: unmatched: step 1 \"J2.failure\" does not fit: the path is at J1 (if in.AmountCents <= 0) [b-unmatched.trace.json]\n",
		"orders.OrderWorkflow: stale: recorded for a different version of the workflow function; re-record it [c-stale.trace.json]\n",
		"orders.OrderWorkflow: incomplete: the run never reached a return (panic, timeout, or stopped) [d-incomplete.trace.json]\n",
		"nope.Nope: unknown workflow: workflow not found in the analyzed packages (or not recordable yet) [e-unknown.trace.json]\n",
		"f-broken.trace.json: could not read trace: not a valid trace file: ",
		"\n6 traces: 1 matched, 1 unmatched, 1 stale, 1 incomplete, 1 unknown workflow, 1 unreadable\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output is missing %q\n--- output:\n%s", want, stdout)
		}
	}
}

// One trace is "1 trace", not "1 traces".
func TestTraceCountWording(t *testing.T) {
	for n, want := range map[int]string{0: "0 traces", 1: "1 trace", 2: "2 traces"} {
		if got := count(n, "trace"); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}

	dir := t.TempDir()
	writeTrace(t, dir, "only", map[string]any{"schemaVersion": 1, "workflow": "nope.Nope", "status": "complete", "steps": []string{}})
	stdout, _, code := run(t, "traces", pilot+"/orders", "--traces", dir)
	if code != ExitOK || !strings.HasSuffix(stdout, "\n1 trace: 1 unknown workflow\n") {
		t.Errorf("code=%d, output does not end with %q:\n%s", code, "1 trace: 1 unknown workflow", stdout)
	}
}
