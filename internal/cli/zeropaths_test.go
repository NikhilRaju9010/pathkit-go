package cli

import (
	"strings"
	"testing"
)

// A workflow with no path at all (a loop with no way out) gets a warning in
// the text report and on stderr. A workflow with at least one path, and the
// JSON, print exactly what they always did.
func TestZeroPathWarning(t *testing.T) {
	zp := absPath(t, fixtures+"/zeropaths/...")
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})
	const headline = "WARNING: PathKit found no decision points to measure in 1 workflow, which has no paths: zeropaths.Spin"

	out, stderr, code := run(t, "analyze", zp)
	if code != ExitOK {
		t.Fatalf("analyze: %d %s", code, stderr)
	}
	if !strings.Contains(stderr, "pathkit analyze: "+headline+"\n") || !strings.Contains(stderr, "What to do:") {
		t.Errorf("analyze stderr should carry the warning:\n%s", stderr)
	}
	if strings.Contains(out, "WARNING") || !strings.Contains(out, "Workflow: zeropaths.Spin\nTotal paths: 0\n") {
		t.Errorf("analyze stdout must stay the plain listing:\n%s", out)
	}

	for _, cmd := range []string{"report", "coverage"} {
		out, stderr, code := run(t, cmd, zp, "--traces", traces)
		if code != ExitOK {
			t.Fatalf("%s: %d %s", cmd, code, stderr)
		}
		if !strings.HasPrefix(out, headline+"\n") || !strings.Contains(out, "What to do:") {
			t.Errorf("%s output should start with the warning:\n%.500s", cmd, out)
		}
		if !strings.Contains(stderr, "pathkit "+cmd+": "+headline+"\n") {
			t.Errorf("%s stderr should carry the warning:\n%s", cmd, stderr)
		}
	}

	// The ordinary workflow next to it, alone: nothing new anywhere.
	for _, cmd := range []string{"analyze", "report", "coverage"} {
		args := []string{cmd, zp, "--include", "Plain"}
		if cmd != "analyze" {
			args = append(args, "--traces", traces)
		}
		out, stderr, code := run(t, args...)
		if code != ExitOK {
			t.Fatalf("%s Plain: %d %s", cmd, code, stderr)
		}
		if strings.Contains(out+stderr, "no decision points") {
			t.Errorf("%s: a workflow with paths must not get the warning:\n%s\n%s", cmd, out, stderr)
		}
	}

	// JSON keeps its keys: no warning field, only the numbers.
	out, stderr, code = run(t, "report", zp, "--traces", traces, "--json")
	if code != ExitOK {
		t.Fatalf("json: %d %s", code, stderr)
	}
	if strings.Contains(out, "no decision points") {
		t.Errorf("--json stdout must not carry the warning:\n%.400s", out)
	}
	_ = decode(t, out)
}
