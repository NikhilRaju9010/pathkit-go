package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// summarizeReport runs "report --json" on the summarize fixture and returns
// the workflow named name.
func summarizeReport(t *testing.T, fixture, traces, cfg string) map[string]any {
	t.Helper()
	args := []string{"report", fixture, "--traces", traces, "--json"}
	if cfg != "" {
		args = append(args, "--config", cfg)
	}
	out, stderr, code := pathkit(t, args...)
	if code != 0 {
		t.Fatalf("report: exit %d\n%s\n%s", code, out, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func workflowNamed(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, w := range doc["workflows"].([]any) {
		if w.(map[string]any)["name"] == name {
			return w.(map[string]any)
		}
	}
	t.Fatalf("no workflow %s", name)
	return nil
}

// A workflow that calls one helper at twelve places has 8191 paths with the
// helper copied in. PathKit maps the helper once instead: the workflow has a
// few paths, the helper's branches are covered by any call of it, and traces
// recorded before the change (with the helper copied in) still count.
func TestSummarizedHelpersCountOldAndNewTraces(t *testing.T) {
	fixture := abs(t, "../../testdata/fixtures/summarize")
	dir := t.TempDir()
	inlineCfg := filepath.Join(dir, "inline.json")
	if err := os.WriteFile(inlineCfg, []byte(`{"classification": {"helpers": {"mode": "inline"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	for _, rec := range []struct{ name, cfg string }{{"recorded with the helpers copied in (what older PathKit did)", inlineCfg}, {"recorded with the helpers summarized", ""}} {
		t.Run(rec.name, func(t *testing.T) {
			traces := filepath.Join(dir, "traces-"+strings.NewReplacer(" ", "-", "(", "", ")", "").Replace(rec.name[:10]))
			args := []string{"test", fixture + "/...", "--traces", traces}
			if rec.cfg != "" {
				args = append(args, "--config", rec.cfg)
			}
			if _, stderr, code := pathkit(t, args...); code != 0 {
				t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
			}
			doc := summarizeReport(t, fixture, traces, "")
			chain := workflowNamed(t, doc, "summarize.Chain")
			if chain["helpersSummarized"] != true {
				t.Fatalf("Chain should have its helpers summarized: %v", chain["helpersReason"])
			}
			if got := chain["paths"].(map[string]any)["total"].(float64); got != 13 {
				t.Errorf("Chain has %v paths, want 13 (8191 with the helper copied in)", got)
			}
			if got := chain["paths"].(map[string]any)["covered"].(float64); got < 1 {
				t.Errorf("Chain: no path covered; the traces were not read (%v)", doc["traces"])
			}
			helpers := chain["helpers"].([]any)
			if len(helpers) != 1 {
				t.Fatalf("Chain helpers: %v", helpers)
			}
			h := helpers[0].(map[string]any)
			b := h["branches"].(map[string]any)
			if h["helper"] != "pause" || h["sites"].(float64) != 12 || b["total"].(float64) != 2 || b["covered"].(float64) != 2 {
				t.Errorf("helper row: %v", h)
			}
			tr := doc["traces"].(map[string]any)
			if tr["stale"].(float64) != 0 || tr["unmatched"].(float64) != 0 {
				t.Errorf("traces: %v", tr)
			}
			// Near miss: a workflow under the cap keeps its helpers copied in.
			small := workflowNamed(t, doc, "summarize.Small")
			if small["helpersSummarized"] == true {
				t.Errorf("Small is under the cap but its helpers were summarized")
			}
		})
	}
}
