package htmlreport

import (
	"crypto/sha256"
	"encoding/base64"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// danger holds every character that means something in HTML.
const danger = `<script>alert("x")</script> & 'q' <b>`

func sample(name, file, reason string) ReportInput {
	return ReportInput{
		Header: Header{Generated: time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC), Folder: "./...", Config: file + ".json"},
		Result: coverage.Result{
			Paths: 3, Covered: 2, Branches: 4, BranchesTaken: 3,
			Workflows: []coverage.WorkflowResult{{
				Name: name, File: file, AddedByConfig: true, Covered: 2, Branches: 4, BranchesTaken: 3,
				Paths: []coverage.PathResult{{Number: 1, Covered: true}, {Number: 2, Covered: true, StaleTrace: true}, {Number: 3}},
			}},
			Excluded: []scope.Excluded{{Name: name + "2", Reason: reason}},
			Counts:   coverage.Counts{Read: 2, Counted: 2},
		},
		ExcludedBy: []string{".pathkitrc.json"},
		Trend:      []Run{{Time: time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC), Paths: 3, Covered: 2, InScope: 1, Excluded: 1, Scope: "a"}},
	}
}

func mustReport(t *testing.T, in ReportInput) string {
	t.Helper()
	page, err := Report(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(page)
}

// The page loads nothing from anywhere: no external stylesheets, fonts,
// images or scripts, links only inside the page, and a Content-Security-
// Policy that allows exactly the page's own script (by its hash) and makes
// the browser refuse any request.
func TestSelfContained(t *testing.T) {
	pages := map[string]string{"report": mustReport(t, sample("p.W", "p/w.go", "why")), "classified": mustReport(t, classified())}
	a, err := Analysis(AnalysisInput{Header: Header{Generated: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	pages["analysis"] = string(a)
	for kind, page := range pages {
		lower := strings.ToLower(page)
		for _, bad := range []string{"<link", "@import", "@font-face", "http:", "https:", "<iframe", "<img", "<object", "<embed", "<form"} {
			if strings.Contains(lower, bad) {
				t.Errorf("%s page contains %q", kind, bad)
			}
		}
		if n := strings.Count(lower, "<script"); n != 2 {
			t.Errorf("%s page has %d script elements, want 2 (the data and the code)", kind, n)
		}
		for _, m := range regexp.MustCompile(`(?i)\bsrc\s*=`).FindAllString(page, -1) {
			t.Errorf("%s page has a src attribute", kind)
			_ = m
		}
		for _, m := range regexp.MustCompile(`(?i)\bhref\s*=\s*"([^"]*)"`).FindAllStringSubmatch(page, -1) {
			if !strings.HasPrefix(m[1], "#") {
				t.Errorf("%s page has a link out of the page: %q", kind, m[1])
			}
		}
		for _, u := range regexp.MustCompile(`url\(([^)]*)\)`).FindAllStringSubmatch(page, -1) {
			if !strings.HasPrefix(strings.Trim(u[1], `'" `), "data:") {
				t.Errorf("%s page has url(%s)", kind, u[1])
			}
		}
		// the one CSP allows the one script by its hash
		code := regexp.MustCompile(`(?s)<script id="pk-code">(.*?)</script>`).FindStringSubmatch(page)
		if code == nil || strings.Count(page, "Content-Security-Policy") != 1 {
			t.Fatalf("%s page: no script or not exactly one CSP meta", kind)
		}
		sum := sha256.Sum256([]byte(code[1]))
		csp := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'sha256-` +
			base64.StdEncoding.EncodeToString(sum[:]) + `'; img-src data:">`
		if !strings.Contains(page, csp) {
			t.Errorf("%s page: the CSP does not name the script's hash: want %s", kind, csp)
		}
		// the script never reaches for the network or builds HTML from data
		for _, bad := range []string{"fetch(", "XMLHttpRequest", "WebSocket", "import(", "eval(", "new Function", "innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "localStorage", "sessionStorage"} {
			if strings.Contains(code[1], bad) {
				t.Errorf("%s page: the script contains %q", kind, bad)
			}
		}
		// light only
		if strings.Contains(lower, "prefers-color-scheme: dark") || !strings.Contains(lower, "color-scheme: light") {
			t.Errorf("%s page is not light only", kind)
		}
	}
}

// Everything that comes from user code or config is escaped: the
// dangerous text never appears raw, every server-written place shows it
// exactly (unescaping gives the original back, so nothing is escaped
// twice), and the data block decodes to the original strings.
func TestEscapesEverything(t *testing.T) {
	name, file, reason := "p.W"+danger, "dir"+danger+"/w.go", "reason "+danger
	page := mustReport(t, sample(name, file, reason))
	for _, raw := range []string{"<script>", "<b>", `alert("x")`, "& 'q'"} {
		if strings.Contains(page, raw) {
			t.Errorf("raw %q in the page", raw)
		}
	}
	if n := strings.Count(page, "</script>"); n != 2 {
		t.Errorf("%d closing script tags, want 2: the data must not close its element", n)
	}
	shown := func(re string) []string {
		var out []string
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(page, -1) {
			out = append(out, html.UnescapeString(m[1]))
		}
		return out
	}
	checks := map[string]string{
		`<summary><h3>(.*?)</h3>`:                              name,
		`<p class="file">(.*?)</p>`:                            file,
		`<span class="reason">(.*?)</span>`:                    reason,
		`data-workflow="(.*?)"`:                                name,
		`data-excluded-workflow="(.*?)"`:                       name + "2",
		`<p class="meta"[^>]*>Generated .*? · (.*?) · PathKit`: "./... · config " + file + ".json",
	}
	for re, want := range checks {
		got := shown(re)
		if len(got) == 0 {
			t.Errorf("%s: not found", re)
		}
		for _, g := range got {
			if g != want {
				t.Errorf("%s shows %q, want %q", re, g, want)
			}
		}
	}
	d := decodeData(t, page)
	if got := d.W[0].N; got != name {
		t.Errorf("data block: workflow name %q, want %q", got, name)
	}
	if got := d.W[0].F; got != strings.ReplaceAll(file, "\\", "/") {
		t.Errorf("data block: file %q", got)
	}
}

// No value is ever marked "safe" for html/template: that would switch its
// escaping off. This scans every Go file of PathKit (not tests) for the
// types that do that.
func TestNothingMarkedSafe(t *testing.T) {
	trusted := map[string]bool{"HTML": true, "HTMLAttr": true, "JS": true, "JSStr": true, "CSS": true, "URL": true, "Srcset": true}
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "template" && trusted[sel.Sel.Name] {
					t.Errorf("%s uses template.%s, which turns escaping off", path, sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A workflow cut at the path cap says "truncated", as the text report does,
// in the page and in the data.
func TestTruncatedShown(t *testing.T) {
	in := sample("p.W", "p/w.go", "why")
	in.Result.Workflows[0].Truncated = true
	raw := mustReport(t, in)
	page := html.UnescapeString(raw) // as the browser shows it ("+" is written &#43;)
	for _, want := range []string{
		`data-truncated="true"`,
		`2/3+ paths (truncated at maxPaths=2000)`,
		`<span class="truncated">truncated</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if !decodeData(t, raw).W[0].Tr {
		t.Error("the data block does not say truncated")
	}
}

func TestAnalysisPageSaysNotMeasured(t *testing.T) {
	page, err := Analysis(AnalysisInput{Header: Header{Generated: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Not measured.", "No workflows excluded"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("analysis page lacks %q", want)
		}
	}
	if strings.Contains(string(page), `class="trend"`) || strings.Contains(string(page), `class="traces"`) {
		t.Errorf("analysis page shows coverage-only sections")
	}
	if decodeData(t, string(page)).M {
		t.Error("the data block says the run was measured")
	}
}

// A missing or damaged history never fails: a warning, and a fresh trend.
func TestReadHistory(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, HistoryFile)
	runs, warn := ReadHistory(missing)
	if runs != nil || warn != "no report history at "+missing+" yet; starting a new trend" {
		t.Errorf("missing: %v %q", runs, warn)
	}
	damaged := map[string]string{
		"not JSON":             `{"schemaVersion": 1, "tool": "pathkit-go", "runs": [`,
		"other tool":           `{"schemaVersion": 1, "tool": "else", "runs": []}`,
		"other version":        `{"schemaVersion": 2, "tool": "pathkit-go", "runs": []}`,
		"unknown field":        `{"schemaVersion": 1, "tool": "pathkit-go", "runs": [], "x": 1}`,
		"impossible run":       `{"schemaVersion": 1, "tool": "pathkit-go", "runs": [{"paths": 3, "covered": 4}]}`,
		"empty file":           ``,
		"a folder, not a file": "DIR",
	}
	for name, content := range damaged {
		p := filepath.Join(t.TempDir(), HistoryFile)
		if content == "DIR" {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runs, warn := ReadHistory(p)
		if runs != nil || !strings.HasSuffix(warn, "; starting a new trend") || !strings.Contains(warn, p) {
			t.Errorf("%s: runs=%v warning=%q", name, runs, warn)
		}
	}
	// A good file round-trips.
	good := []Run{{Time: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), Paths: 3, Covered: 2, InScope: 1, Scope: "x"}}
	if err := WriteHistory(missing, good); err != nil {
		t.Fatal(err)
	}
	if runs, warn := ReadHistory(missing); warn != "" || len(runs) != 1 || runs[0] != good[0] {
		t.Errorf("round trip: %v %q", runs, warn)
	}
}

func TestAddRunKeepsFive(t *testing.T) {
	var runs []Run
	for i := 1; i <= 7; i++ {
		runs = AddRun(runs, Run{Paths: 10, Covered: i})
	}
	if len(runs) != MaxRuns || runs[0].Covered != 3 || runs[4].Covered != 7 {
		t.Errorf("got %v; want the last 5 (3..7)", runs)
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint([]string{"x", "y"}, []string{"z"})
	if a != Fingerprint([]string{"y", "x"}, []string{"z"}) {
		t.Error("order changed the fingerprint")
	}
	if a == Fingerprint([]string{"x", "y", "z"}, nil) || a == Fingerprint([]string{"x"}, []string{"y", "z"}) {
		t.Error("a different scope gave the same fingerprint")
	}
}

// When the scope changes between runs, the trend says so.
func TestTrendMarksScopeChange(t *testing.T) {
	in := sample("p.W", "p/w.go", "why")
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	in.Trend = []Run{
		{Time: t0, Paths: 38, Covered: 20, InScope: 8, Scope: "all"},
		{Time: t0.Add(time.Hour), Paths: 38, Covered: 20, InScope: 8, Scope: "all"},
		{Time: t0.Add(2 * time.Hour), Paths: 29, Covered: 17, InScope: 7, Excluded: 1, Scope: "no-shipment"},
	}
	page := mustReport(t, in)
	if n := strings.Count(page, `class="scope-changed"`); n != 1 {
		t.Errorf("%d scope-changed marks, want 1 (only the third run)", n)
	}
	for _, want := range []string{"scope changed: 7 in scope, 1 excluded", "Trend (last 3 report runs)", "17/29 · 58.6%"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

// The page made for people who test: the branch coverage card, what to test
// next, the workflows worst first, a checklist, "More detail" closed, and a
// glossary entry for every "i" marker (the page would not render otherwise).
func TestPageForPeople(t *testing.T) {
	page := mustReport(t, sample("p.W", "p/w.go", "why"))
	for _, want := range []string{`id="overview"`, `Branch coverage`, `id="next"`, `What to test next`, `id="workflow-list"`, `Decision checklist`,
		`<details id="more-detail" class="more">`, `id="glossary"`, `data-tip="branch-coverage"`, `aria-describedby="g-branch-coverage"`, `id="g-branch-coverage"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, `<details id="more-detail" class="more" open`) {
		t.Error("More detail is closed by default")
	}
	// every marker points at a glossary entry on the page
	for _, m := range regexp.MustCompile(`data-tip="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		if !strings.Contains(page, `id="g-`+m[1]+`"`) {
			t.Errorf("the marker %q has no glossary entry", m[1])
		}
	}
	// status is an icon and a word, never colour alone
	for _, w := range []string{"Tested", "Missed"} {
		if !strings.Contains(page, ">"+w+"</span>") && !strings.Contains(page, "</span> "+w+"</span>") && !strings.Contains(page, w) {
			t.Errorf("no %q status word", w)
		}
	}
	// near miss: the page of analyze has no tests to measure, so no coverage card
	a, err := Analysis(AnalysisInput{Header: Header{Generated: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(a), `id="overview"`) {
		t.Error("the analyze page has no branch coverage card")
	}
}

func TestEveryTermIsDefinedOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, tm := range terms {
		if tm.Key == "" || tm.Term == "" || tm.Text == "" || seen[tm.Key] {
			t.Errorf("bad or repeated term %+v", tm)
		}
		seen[tm.Key] = true
	}
	if _, err := tipFor("no-such-term"); err == nil {
		t.Error("an unknown term must be an error, not an empty tooltip")
	}
}

// The decisions that run under a disconnected context are a block of their own
// in the checklist, after the others, with a heading that says what it is.
func TestChecklistShowsCleanupAfterACancel(t *testing.T) {
	in := sample("p.W", "p/w.go", "why")
	in.Result.Workflows[0].Outcomes = []coverage.OutcomeResult{
		{ID: "b:1", File: "p/w.go", Line: 30, Func: "W", Decision: "Cleanup (activity)", Outcome: "failure", Kind: model.ErrCheck, Status: coverage.OutcomeMissed, Cleanup: true, Sites: 1},
		{ID: "b:2", File: "p/w.go", Line: 20, Func: "W", Decision: "Work (activity)", Outcome: "failure", Kind: model.ErrCheck, Status: coverage.OutcomeCovered, Sites: 1},
	}
	page := mustReport(t, in)
	work, head := strings.Index(page, "When Work finishes"), strings.Index(page, "Cleanup after a cancel: the decisions below")
	clean := strings.Index(page, "When Cleanup finishes")
	if work < 0 || head < 0 || clean < 0 || !(work < head && head < clean) {
		t.Errorf("the cleanup block should come after the work, with its heading (work %d, heading %d, cleanup %d)", work, head, clean)
	}
}

func TestNextListShowsNoZeroCounts(t *testing.T) {
	in := sample("p.W", "p/w.go", "why")
	in.Outcomes = []advice.OutcomeItem{
		// an outcome of a helper: no listed path goes through it
		{Rank: 1, Workflow: "p.W", File: "p/w.go", Line: 30, Risk: advice.TagFailure, Sentence: "Work never failed here", TestName: "TestW_WorkFails"},
		{Rank: 2, Workflow: "p.W", File: "p/w.go", Line: 40, Risk: advice.TagLogic, Sentence: "`x` was never true", TestName: "TestW_XTrue", Paths: 3, Scenarios: 2},
	}
	page := mustReport(t, in)
	if strings.Contains(page, "would cover 0 path(s)") {
		t.Error("a count of zero is shown")
	}
	if !strings.Contains(page, "would cover 3 path(s), 2 scenario(s)") {
		t.Error("the count of an outcome with paths is missing")
	}
	if strings.Count(page, "A test of it would cover") != 1 {
		t.Error("only the outcome with paths gets a count")
	}
}
