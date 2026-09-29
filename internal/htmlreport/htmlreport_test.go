package htmlreport

import (
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

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
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

// The page loads nothing from anywhere: no scripts at all, no external
// stylesheets, fonts or images, and a Content-Security-Policy that makes
// the browser refuse any request.
func TestSelfContained(t *testing.T) {
	pages := map[string]string{"report": mustReport(t, sample("p.W", "p/w.go", "why"))}
	a, err := Analysis(AnalysisInput{Header: Header{Generated: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	pages["analysis"] = string(a)
	for kind, page := range pages {
		lower := strings.ToLower(page)
		for _, bad := range []string{"<script", "<link", "@import", "@font-face", "http:", "https:", "<iframe", "<img", "<object", "<embed"} {
			if strings.Contains(lower, bad) {
				t.Errorf("%s page contains %q", kind, bad)
			}
		}
		if m := regexp.MustCompile(`(?i)\b(src|href)\s*=`).FindString(page); m != "" {
			t.Errorf("%s page has a %s attribute", kind, m)
		}
		for _, u := range regexp.MustCompile(`url\(([^)]*)\)`).FindAllStringSubmatch(page, -1) {
			if !strings.HasPrefix(strings.Trim(u[1], `'" `), "data:") {
				t.Errorf("%s page has url(%s)", kind, u[1])
			}
		}
		csp := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">`
		if strings.Count(page, "Content-Security-Policy") != 1 || !strings.Contains(page, csp) {
			t.Errorf("%s page lacks exactly one CSP meta: %s", kind, csp)
		}
	}
}

// Everything that comes from user code or config is escaped: the
// dangerous text never appears raw, and every place shows it exactly
// (unescaping gives the original back, so nothing is escaped twice).
func TestEscapesEverything(t *testing.T) {
	name, file, reason := "p.W"+danger, "dir"+danger+"/w.go", "reason "+danger
	page := mustReport(t, sample(name, file, reason))
	for _, raw := range []string{"<script>", "<b>", `alert("x")`, "& 'q'"} {
		if strings.Contains(page, raw) {
			t.Errorf("raw %q in the page", raw)
		}
	}
	shown := func(re string) []string {
		var out []string
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(page, -1) {
			out = append(out, html.UnescapeString(m[1]))
		}
		return out
	}
	checks := map[string]string{
		`<h3>(.*?)</h3>`:                            name,
		`<span class="file">(.*?)</span>`:           file,
		`<span class="reason">(.*?)</span>`:         reason,
		`data-workflow="(.*?)"`:                     name,
		`data-excluded-workflow="(.*?)"`:            name + "2",
		`<p class="meta">Generated .*? · (.*?)</p>`: "./... · config " + file + ".json",
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

// A workflow cut at the path cap says "truncated", as the text report does.
func TestTruncatedShown(t *testing.T) {
	in := sample("p.W", "p/w.go", "why")
	in.Result.Workflows[0].Truncated = true
	page := html.UnescapeString(mustReport(t, in)) // as the browser shows it ("+" is written &#43;)
	for _, want := range []string{
		`data-truncated="true"`,
		`2/3+ paths (truncated at maxPaths=2000)`,
		`<span class="truncated">truncated</span>`,
		`Total paths: 3+ (truncated at maxPaths=2000)`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestAnalysisPageSaysNotMeasured(t *testing.T) {
	page, err := Analysis(AnalysisInput{Header: Header{Generated: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="tab-analysis" checked`, "Not measured.", "No workflows excluded"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("analysis page lacks %q", want)
		}
	}
	if strings.Contains(string(page), `class="trend"`) || strings.Contains(string(page), `class="traces"`) {
		t.Errorf("analysis page shows coverage-only sections")
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
