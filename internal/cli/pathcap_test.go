package cli

import (
	"html"
	"path/filepath"
	"strings"
	"testing"
)

// A workflow cut at the path cap gets a loud warning at the top of the text
// report, in the JSON (an additive field) and in the HTML page; a project
// under the cap prints exactly what it always did.
func TestPathCapWarning(t *testing.T) {
	rules := absPath(t, fixtures+"/rules/...")
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})

	for _, cmd := range []string{"report", "coverage"} {
		out, stderr, code := run(t, cmd, rules, "--include", "TwelveIfs", "--traces", traces, "--summary")
		if cmd == "coverage" { // coverage has no --summary
			out, stderr, code = run(t, cmd, rules, "--include", "TwelveIfs", "--traces", traces)
		}
		if code != ExitOK {
			t.Fatalf("%s: %d %s", cmd, code, stderr)
		}
		if !strings.HasPrefix(out, "WARNING: 1 workflow has more than 2000 paths, so its path list and classification are incomplete: rules.TwelveIfs\n") ||
			!strings.Contains(out, "What to do: split the workflow") {
			t.Errorf("%s output should start with the cap warning:\n%.600s", cmd, out)
		}
		head, _, _ := strings.Cut(out, "\n\n") // the warning block
		if strings.Contains(head, "stale") {
			t.Errorf("%s: a path beyond the cap must not be called stale:\n%s", cmd, head)
		}
	}

	out, stderr, code := run(t, "report", rules, "--include", "TwelveIfs", "--traces", traces, "--summary", "--json")
	if code != ExitOK {
		t.Fatalf("json: %d %s", code, stderr)
	}
	w, ok := decode(t, out)["pathCapWarning"].(map[string]any)
	if !ok || w["limit"] != 2000.0 || len(w["workflows"].([]any)) != 1 || !strings.Contains(w["message"].(string), "incomplete") {
		t.Errorf("pathCapWarning: %v", w)
	}

	page := filepath.Join(t.TempDir(), "r.html")
	if _, stderr, code := run(t, "report", rules, "--include", "TwelveIfs", "--traces", traces, "--summary", "--html="+page); code != ExitOK {
		t.Fatalf("html: %d %s", code, stderr)
	}
	if got := html.UnescapeString(readFile(t, page)); !strings.Contains(got, `class="cap-warning" role="alert"`) || !strings.Contains(got, "path list and classification are incomplete") {
		t.Errorf("HTML page lacks the cap warning")
	}

	// analyze keeps its listing as it was and warns on stderr.
	aout, astderr, acode := run(t, "analyze", rules, "--include", "TwelveIfs", "--summary")
	if acode != ExitOK || !strings.HasPrefix(aout, "Workflow: rules.TwelveIfs\n") || !strings.Contains(astderr, "path list and classification are incomplete") {
		t.Errorf("analyze: code=%d stderr=%q stdout=%.100q", acode, astderr, aout)
	}
}

// Nothing is added when no workflow reaches the cap.
func TestNoPathCapWarningUnderTheCap(t *testing.T) {
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})
	out, stderr, code := run(t, "report", pilot+"/orders", "--traces", traces, "--json")
	if code != ExitOK {
		t.Fatalf("%d %s", code, stderr)
	}
	if _, has := decode(t, out)["pathCapWarning"]; has {
		t.Errorf("pathCapWarning present for a workflow under the cap")
	}
	text, _, _ := run(t, "report", pilot+"/orders", "--traces", traces)
	if strings.Contains(text, "WARNING") {
		t.Errorf("unexpected warning:\n%s", text)
	}
	page := filepath.Join(t.TempDir(), "r.html")
	run(t, "report", pilot+"/orders", "--traces", traces, "--html="+page)
	if strings.Contains(readFile(t, page), "cap-warning") {
		t.Errorf("HTML page has a cap warning under the cap")
	}
}
