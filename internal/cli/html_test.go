package cli

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/htmlreport"
)

// fixedClock makes the page's "Generated" time and the trend repeatable.
func fixedClock(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// The whole orders page, pinned (fixed clock, first run of its trend).
func TestReportHTMLGolden(t *testing.T) {
	fixedClock(t)
	traces := ordersTraces(t)
	golden := absPath(t, "testdata/report_orders.html")
	out := filepath.Join(t.TempDir(), "report.html")
	inPilot(t)
	stdout, stderr, code := run(t, "report", "./orders", "--traces", traces, "--html="+out)
	if code != ExitOK || stdout != native(ordersReport) {
		t.Fatalf("code=%d; --html must not change stdout:\n%s", code, stdout)
	}
	hist := filepath.Join(filepath.Dir(out), "report-history.json")
	wantErr := "pathkit report: warning: no report history at " + hist + " yet; starting a new trend\npathkit report: wrote " + out + "\n"
	if stderr != wantErr {
		t.Errorf("stderr %q\nwant   %q", stderr, wantErr)
	}
	if got := readFile(t, out); got != readFile(t, golden) {
		t.Errorf("the page differs from testdata/report_orders.html:\n%s", got)
	}
}

// The trend keeps the last 5 report runs, and marks a scope change.
func TestReportHTMLTrend(t *testing.T) {
	fixedClock(t)
	traces := ordersTraces(t)
	out := filepath.Join(t.TempDir(), "report.html")
	inPilot(t)
	for i := 0; i < 6; i++ {
		if _, stderr, code := run(t, "report", "./...", "--traces", traces, "--html="+out); code != ExitOK {
			t.Fatalf("run %d: code=%d %s", i, code, stderr)
		}
	}
	runs, warning := htmlreport.ReadHistory(htmlreport.HistoryPath(out))
	if warning != "" || len(runs) != 5 {
		t.Fatalf("%d runs, warning %q; want 5", len(runs), warning)
	}
	if page := readFile(t, out); !strings.Contains(page, "Trend (last 5 report runs)") || strings.Contains(page, `class="scope-changed"`) {
		t.Errorf("trend wrong after 6 identical runs")
	}
	run(t, "report", "./...", "--traces", traces, "--html="+out, "--exclude", "ShipmentWorkflow")
	page := readFile(t, out)
	if strings.Count(page, `class="scope-changed"`) != 1 || !strings.Contains(page, "scope changed: 7 in scope, 1 excluded") {
		t.Errorf("the scope change is not marked:\n%s", page[strings.Index(page, `class="trend"`):])
	}
}

// Owner's requirement (M8): a missing or damaged report-history.json gives
// a warning and starts fresh; it never fails the report.
func TestReportHTMLHistoryMissingOrDamaged(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	for name, content := range map[string]string{"missing": "", "damaged": `{"schemaVersion": 1, "runs": [`, "garbage": "\x00\x01 not json"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out, hist := filepath.Join(dir, "r.html"), filepath.Join(dir, "report-history.json")
			if content != "" {
				if err := os.WriteFile(hist, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stdout, stderr, code := run(t, "report", "./orders", "--traces", traces, "--html="+out, "--fail-under", "10")
			if code != ExitOK || !strings.Contains(stdout, "66.7% project coverage") || !exists(out) {
				t.Fatalf("code=%d, the report must not fail: %s", code, stderr)
			}
			if !strings.Contains(stderr, "pathkit report: warning: ") || !strings.Contains(stderr, hist) || !strings.Contains(stderr, "; starting a new trend\n") {
				t.Errorf("no warning naming the file: %q", stderr)
			}
			if runs, warning := htmlreport.ReadHistory(hist); warning != "" || len(runs) != 1 {
				t.Errorf("the fresh trend: %d runs, %q; want 1 run and a readable file", len(runs), warning)
			}
		})
	}
}

// Owner's requirement (M8): pathkit clean and --clean never delete
// report-history.json, even when it sits in the trace folder. (pathkit
// test is checked end to end in internal/e2e, TestCoverageMatchesKey.)
func TestHistorySurvivesCleanup(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	out := filepath.Join(traces, "report.html") // history lands in the trace folder itself
	hist := htmlreport.HistoryPath(out)
	if _, _, code := run(t, "report", "./orders", "--traces", traces, "--html="+out, "--clean"); code != ExitOK || !exists(hist) {
		t.Fatalf("--clean: code=%d, history exists=%v", code, exists(hist))
	}
	if files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json")); len(files) != 0 {
		t.Fatalf("--clean left %d traces", len(files))
	}
	before := readFile(t, hist)
	writeTrace(t, traces, "again", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})
	for _, args := range [][]string{{"clean", "--traces", traces}, {"clean", "--traces", traces, "--older-than", "1m"}} {
		if _, stderr, code := run(t, args...); code != ExitOK {
			t.Fatalf("%v: code=%d %s", args, code, stderr)
		}
		if !exists(hist) || readFile(t, hist) != before || !exists(out) {
			t.Errorf("%v deleted or changed the history or the page", args)
		}
	}
}

// The html config key: report only, relative to the config file, and the
// flag always wins.
func TestReportHTMLConfigKey(t *testing.T) {
	traces := ordersTraces(t)
	orders := absPath(t, pilot+"/orders")
	cwd := t.TempDir()
	t.Chdir(cwd)
	tests := []struct {
		name, config string
		args         []string
		want         string // relative to the config's folder ("@") or the current folder
		none         bool
	}{
		{"true: the default file, from the current folder", `{"html": true}`, nil, filepath.Join(".pathkit", "report.html"), false},
		{"a path: relative to the config file", `{"html": "out/r.html"}`, nil, "@/out/r.html", false},
		{"false: no file", `{"html": false}`, nil, "", true},
		{"--html=false beats true", `{"html": true}`, []string{"--html=false"}, "", true},
		{"--html=path beats the config", `{"html": "out/r.html"}`, []string{"--html=" + filepath.Join(cwd, "flag.html")}, filepath.Join(cwd, "flag.html"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.RemoveAll(filepath.Join(cwd, ".pathkit"))
			cfg := writeConfig(t, tt.config)
			want := strings.Replace(tt.want, "@", filepath.Dir(cfg), 1)
			_, stderr, code := run(t, append([]string{"report", orders, "--traces", traces, "--config", cfg}, tt.args...)...)
			if code != ExitOK {
				t.Fatalf("code=%d %s", code, stderr)
			}
			if tt.none {
				if strings.Contains(stderr, "wrote") || exists(filepath.Join(cwd, ".pathkit", "report.html")) {
					t.Errorf("an HTML file was written: %s", stderr)
				}
				return
			}
			if !exists(want) || !strings.Contains(stderr, "pathkit report: wrote "+want+"\n") {
				t.Errorf("want %s written; stderr %q", want, stderr)
			}
		})
	}
	// analyze ignores the config's html (it is for report, D12).
	cfg := writeConfig(t, `{"html": true}`)
	if _, stderr, _ := run(t, "analyze", orders, "--config", cfg); strings.Contains(stderr, "wrote") || exists(filepath.Join(cwd, ".pathkit", "analysis.html")) {
		t.Errorf("analyze applied the config's html: %s", stderr)
	}
}

// Exit 2 still writes the page; exit 1 writes none; a page that can't be
// written is exit 1 and keeps the traces.
func TestReportHTMLExitCodes(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "r.html")
	if _, _, code := run(t, "report", "./orders", "--traces", traces, "--html="+out, "--fail-under", "90"); code != ExitBelowThreshold || !exists(out) {
		t.Errorf("exit 2: code=%d, written=%v", code, exists(out))
	}
	out1 := filepath.Join(dir, "none.html")
	if _, _, code := run(t, "report", absPath(t, fixtures+"/rules/..."), "--traces", traces, "--html="+out1); code != ExitError || exists(out1) {
		t.Errorf("exit 1: code=%d, written=%v", code, exists(out1))
	}
	blocker := filepath.Join(dir, "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	stdout, stderr, code := run(t, "report", "./orders", "--traces", traces, "--html="+filepath.Join(blocker, "r.html"), "--clean")
	files, _ := filepath.Glob(filepath.Join(traces, "*.trace.json"))
	if code != ExitError || !strings.Contains(stderr, "could not write --html file") || len(files) != 2 || !strings.Contains(stdout, "project coverage") {
		t.Errorf("unwritable: code=%d stderr=%q, %d traces left (want 2 kept)", code, stderr, len(files))
	}
}

// "--html out.html" (with a space) reads out.html as the folder argument.
func TestHTMLSpaceHint(t *testing.T) {
	for _, command := range []string{"report", "analyze"} {
		_, stderr, code := run(t, command, "--html", "out.html")
		want := "pathkit " + command + ": out.html was read as the folder argument; to name the HTML file, write --html=out.html (with =)\n"
		if code != ExitError || stderr != want {
			t.Errorf("%s: code=%d stderr=%q", command, code, stderr)
		}
	}
}

func TestAnalyzeHTML(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.html")
	stdout, stderr, code := run(t, "analyze", pilot+"/orders", "--summary", "--html="+out)
	if code != ExitOK || !strings.Contains(stdout, "Total paths: 3") || stderr != "pathkit analyze: wrote "+out+"\n" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	page := readFile(t, out)
	// The page has the full listing, whatever --summary says.
	if strings.Count(page, `<code class="path">`) != 3 || !strings.Contains(page, "Not measured.") {
		t.Errorf("analysis page:\n%s", page)
	}
}

// Owner's requirement (M8): a workflow cut at the path cap shows
// "truncated" in the HTML, as the text report does.
func TestHTMLTruncated(t *testing.T) {
	rules := absPath(t, fixtures+"/rules/...")
	dir := t.TempDir()
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})
	a, r := filepath.Join(dir, "a.html"), filepath.Join(dir, "r.html")
	if _, stderr, code := run(t, "analyze", rules, "--include", "TwelveIfs", "--summary", "--html="+a); code != ExitOK {
		t.Fatalf("analyze: %d %s", code, stderr)
	}
	if _, stderr, code := run(t, "report", rules, "--include", "TwelveIfs", "--traces", traces, "--html="+r); code != ExitOK {
		t.Fatalf("report: %d %s", code, stderr)
	}
	for file, wants := range map[string][]string{
		a: {"Total paths: 2000+ (truncated at maxPaths=2000)", `<span class="truncated">truncated</span>`},
		r: {`data-truncated="true"`, "0/2000+ paths (truncated at maxPaths=2000)", "Total paths: 2000+ (truncated at maxPaths=2000)", `<span class="truncated">truncated</span>`},
	} {
		page := html.UnescapeString(readFile(t, file))
		for _, want := range wants {
			if !strings.Contains(page, want) {
				t.Errorf("%s lacks %q", filepath.Base(file), want)
			}
		}
	}
}

// The escaping fixture: every path, as analyze prints it, appears in both
// pages escaped, and unescapes to exactly the original (so nothing is
// escaped twice: the case "&amp;" must come back as &amp;). The config's
// HTML-looking reason is escaped too.
func TestHTMLEscapesUserCode(t *testing.T) {
	pkg := absPath(t, fixtures+"/escaping")
	reason := `<script>alert("x")</script> & 'quotes'`
	cfg := writeConfig(t, `{"packages": [`+jsonString(pkg)+`], "workflows": {"exclude": [{"name": "OtherWorkflow", "reason": `+jsonString(reason)+`}]}}`)
	traces := t.TempDir()
	writeTrace(t, traces, "x", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "x.Y", "status": "complete"})
	dir := t.TempDir()
	a, r := filepath.Join(dir, "a.html"), filepath.Join(dir, "r.html")

	text, stderr, code := run(t, "analyze", pkg, "--config", cfg, "--html="+a)
	if code != ExitOK {
		t.Fatalf("analyze: %d %s", code, stderr)
	}
	var want []string
	for _, line := range strings.Split(text, "\n") {
		if m := regexp.MustCompile(`^  \d+\. (.*)$`).FindStringSubmatch(line); m != nil {
			want = append(want, m[1])
		}
	}
	if len(want) != 6 {
		t.Fatalf("analyze printed %d paths, want 6:\n%s", len(want), text)
	}
	if _, stderr, code := run(t, "report", pkg, "--config", cfg, "--traces", traces, "--html="+r); code != ExitOK {
		t.Fatalf("report: %d %s", code, stderr)
	}
	for _, file := range []string{a, r} {
		page := readFile(t, file)
		for _, raw := range []string{"<b>", "<script>", `"x"`, "& 'y'", "a<b", "c>d", "sig<&>"} {
			if strings.Contains(page, raw) {
				t.Errorf("%s: raw %q in the page", filepath.Base(file), raw)
			}
		}
		var got []string
		for _, m := range regexp.MustCompile(`<code class="path">(.*?)</code>`).FindAllStringSubmatch(page, -1) {
			got = append(got, html.UnescapeString(m[1]))
		}
		// report.html has each path twice (Coverage and Analysis tabs).
		copies := map[string]int{a: 1, r: 2}[file]
		if len(got) != copies*len(want) {
			t.Fatalf("%s: %d paths shown, want %d", filepath.Base(file), len(got), copies*len(want))
		}
		for i, g := range got {
			if w := want[i%len(want)]; g != w {
				t.Errorf("%s path %d shows\n  %q\nwant\n  %q", filepath.Base(file), i%len(want)+1, g, w)
			}
		}
		if !strings.Contains(page, "&amp;amp;") {
			t.Errorf("%s: the literal &amp; in the code is not shown as &amp;", filepath.Base(file))
		}
		m := regexp.MustCompile(`<span class="reason">(.*?)</span>`).FindStringSubmatch(page)
		if m == nil || html.UnescapeString(m[1]) != reason {
			t.Errorf("%s: the reason is not shown exactly: %v", filepath.Base(file), m)
		}
	}
}

// The allowStale config key (applied since M6; its test added in M8): a
// trace from an older version of the code counts with it, not without.
func TestAllowStaleConfigKey(t *testing.T) {
	traces := t.TempDir()
	writeTrace(t, traces, "old", map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
		"functionHash": "0000000000000000", "status": "complete", "steps": []string{"J1.true"}})
	on, off := writeConfig(t, `{"allowStale": true}`), writeConfig(t, `{"allowStale": false}`)
	for _, command := range []string{"coverage", "report"} {
		stdout, _, code := run(t, command, pilot+"/orders", "--traces", traces, "--config", on)
		if code != ExitOK || !strings.Contains(stdout, "(stale trace)") || !strings.Contains(stdout, "1 counted · 0 unmatched · 0 stale") {
			t.Errorf("%s with allowStale: code=%d\n%s", command, code, stdout)
		}
		stdout, stderr, _ := run(t, command, pilot+"/orders", "--traces", traces, "--config", off)
		if strings.Contains(stdout, "(stale trace)") || !strings.Contains(stdout, "0 counted · 0 unmatched · 1 stale") || !strings.Contains(stderr, "stale") {
			t.Errorf("%s without allowStale:\n%s\n%s", command, stdout, stderr)
		}
	}
}

// The noColor config key (applied since M7; its test added in M8), with
// stdout pretending to be a terminal.
func TestNoColorConfigKey(t *testing.T) {
	old := isTerminal
	isTerminal = func(any) bool { return true }
	t.Cleanup(func() { isTerminal = old })
	t.Setenv("NO_COLOR", "")
	traces := ordersTraces(t)
	inPilot(t)
	on := writeConfig(t, `{"noColor": true}`)
	tests := []struct {
		name  string
		args  []string
		color bool
	}{
		{"no config: colour", nil, true},
		{"noColor: none", []string{"--config", on}, false},
		{"--no-color=false beats noColor", []string{"--config", on, "--no-color=false"}, true},
	}
	for _, tt := range tests {
		stdout, _, code := run(t, append([]string{"report", "./orders", "--traces", traces}, tt.args...)...)
		if code != ExitOK || strings.Contains(stdout, "\x1b[") != tt.color {
			t.Errorf("%s: code=%d, colour=%v", tt.name, code, strings.Contains(stdout, "\x1b["))
		}
	}
}
