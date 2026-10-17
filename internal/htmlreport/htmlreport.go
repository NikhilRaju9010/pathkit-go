// Package htmlreport writes PathKit's shareable HTML report (docs/DESIGN.md
// D12): one self-contained file with a summary, a workflow overview, the
// paths of every workflow in paginated tables, the glossary and the
// detector audit.
//
// Rules:
//   - It only lays out numbers and sentences computed elsewhere: coverage
//     results come from the shared measure (coverage.Compute) and every
//     sentence from package render, exactly as the terminal prints it.
//   - It uses html/template, which escapes every inserted value for where
//     it appears, including the data block (a Go value, encoded as JSON for
//     a script). No value is ever marked "safe" (template.HTML and its
//     relatives are never used; a test scans the code for them). The
//     stylesheet and the script are part of the template text itself.
//   - It loads nothing from anywhere: no fonts, stylesheets, images or
//     scripts from a network. The one script is inline and small, and the
//     Content-Security-Policy allows exactly that script by its hash and
//     makes the browser refuse any network request.
//   - The paths are not written as HTML. They are embedded once as compact
//     JSON and the script draws only the page being looked at, with
//     textContent only. Without JavaScript the summary, the workflow
//     overview, the glossary and the numbers are still there, and the page
//     says that the path tables need JavaScript.
package htmlreport

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"html/template"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/version"
)

var (
	//go:embed page.html.tmpl
	pageTemplate string
	//go:embed style.css
	style string
	//go:embed app.js
	script string

	page = template.Must(template.New("page").Parse(strings.Replace(strings.Replace(pageTemplate, "/*STYLE*/", style, 1), "/*SCRIPT*/", script, 1)))
)

// Header is what the top of the page says about the run.
type Header struct {
	Generated time.Time
	Folder    string // what was loaded
	Config    string // the config file used, "" for none
}

// ReportInput is one report run: the shared measure's result and what
// the text report prints around it.
type ReportInput struct {
	Header
	Result     coverage.Result
	Threshold  *float64 // --fail-under or the config's failUnder; nil for none
	ExcludedBy []string // render.ExcludedLine's sources
	Trend      []Run    // oldest first, this run last
	// Classification, when not nil, adds the classification block (the same
	// lines the text report prints) and makes --fail-under use its basis.
	Classification *render.ClassificationView
	// CustomLabels are the config's custom label definitions, shown in the
	// glossary next to the built-in ones.
	CustomLabels []classify.Definition
	// Assumptions are the assumptions that are off, or that claims rest on
	// (classify.Engine.Assumptions); shown in the glossary.
	Assumptions []classify.AssumptionStatus
}

// overallThreshold is the threshold for the overall figures: none when
// the testable basis is the one --fail-under checks (see cli's
// measured.overallThreshold).
func overallThreshold(in ReportInput) *float64 {
	if in.Classification != nil && in.Classification.Basis == coverage.BasisTestable {
		return nil
	}
	return in.Threshold
}

// AnalysisWorkflow is one workflow as analyze lists it.
type AnalysisWorkflow struct {
	Name          string
	File          string
	AddedByConfig bool
	Paths         model.PathList
}

// AnalysisInput is one analyze run.
type AnalysisInput struct {
	Header
	Workflows       []AnalysisWorkflow
	ExcludedHeading string // analyze's own excluded line
	Excluded        []scope.Excluded
}

// The template's data. Every string in it is plain text; the template
// escapes it.
type (
	view struct {
		Title, Generated, Folder, ConfigNote, Version string
		CapHeadline                                   string   // set when a workflow reached the path cap
		CapAdvice                                     []string // what that means and what to do
		Measured                                      bool
		TotalPaths, TotalCovered                      int
		TotalLine, BranchesLine, TracesLine           string
		Explain                                       string // what "testable" and "overall" mean
		Cards                                         []cardView
		Buckets                                       []bucketView
		ClassLines                                    []string
		FailUnder                                     *failUnderView
		Workflows                                     []workflowView
		ExcludedLine                                  string
		Excluded                                      []scope.Excluded
		Traces                                        *coverage.Counts
		Trend                                         []trendView
		Glossary                                      *glossaryView
		Data                                          *pageData
		ScriptHash                                    string
	}
	cardView struct {
		Key, Title, Value, Sub string
		Covered, Total         int
	}
	bucketView struct {
		Key, Name        string
		Paths, Covered   int
		Width            float64 // percent of all paths
		TestableCoverage bool
	}
	workflowView struct {
		Name, File, Stats, TotalPathsLine, Priority, Pct string
		AddedByConfig, Truncated                         bool
		Covered, Total, Missed                           int
		Width                                            float64 // covered, percent of the workflow's paths
		Open                                             bool
		// Exact, ExactLine and Stages are set for a workflow with several stages or
		// more paths than the list holds: the exact count, what it means, and one
		// row per stage.
		Exact, ExactLine string
		Stages           []stageRowView
		PrunedLine       string
	}
	stageRowView struct {
		Number, Paths, Covered, Pct, Branches, Note string
	}
	failUnderView struct {
		Passed bool
		Text   string
	}
	trendView struct {
		Time, Pct, ScopeChanged string
		Covered, Paths          int
		Percent                 float64
	}
)

// Report renders a report run's page.
func Report(in ReportInput) ([]byte, error) {
	res := in.Result
	v := view{
		Title: "PathKit report", Measured: true,
		TotalPaths: res.Paths, TotalCovered: res.Covered,
		TotalLine:    render.ProjectTotalLine(res, overallThreshold(in)),
		BranchesLine: render.BranchesLine(res),
		TracesLine:   strings.TrimSuffix(render.TracesLine(res.Counts), "\n"),
		ExcludedLine: strings.TrimSuffix(render.ExcludedLine(len(res.Excluded), in.ExcludedBy), ":"),
		Excluded:     res.Excluded,
		Traces:       &res.Counts,
	}
	v.setHeader(in.Header)
	if names := render.CapNames(res); len(names) > 0 {
		v.CapHeadline, v.CapAdvice = render.CapHeadline(names), render.CapAdvice()
	}
	basis := coverage.BasisOverall
	if in.Classification != nil {
		basis = in.Classification.Basis
		v.ClassLines = render.ClassificationLines(res, *in.Classification)
	}
	if in.Threshold != nil {
		c := res.Breakdown.For(basis)
		pct := coverage.Percent(c.Covered, c.Paths)
		f := &failUnderView{Passed: pct >= *in.Threshold}
		if f.Passed {
			f.Text = "--fail-under " + render.Threshold(*in.Threshold) + ": passed (" + render.Pct(pct, in.Threshold) + ")"
		} else {
			f.Text = "--fail-under " + render.Threshold(*in.Threshold) + ": coverage " + render.Pct(pct, in.Threshold) + " is below it (exit code 2)"
		}
		if in.Classification != nil {
			f.Text += " (basis: " + string(basis) + ")"
		}
		v.FailUnder = f
	}
	v.buildCards(res, in.Classification != nil)

	if in.Classification != nil {
		v.Glossary = buildGlossary(res, in.CustomLabels, in.Assumptions)
	}
	data := newPageData(true, in.Classification != nil)
	for wi, w := range res.Workflows {
		wv := workflowView{
			Name: w.Name, File: filepath.ToSlash(w.File), AddedByConfig: w.AddedByConfig, Truncated: w.Truncated,
			Stats: render.WorkflowStats(w), Covered: w.Covered, Total: len(w.Paths), Missed: len(w.Paths) - w.Covered,
			TotalPathsLine: render.TotalLine(model.PathList{List: make([]model.Path, len(w.Paths)), Truncated: w.Truncated}, model.DefaultMaxPaths),
			Open:           wi == 0,
		}
		if render.ShowStages(w) {
			wv.Exact = render.Big(w.Exact)
			wv.ExactLine = strings.TrimSuffix(strings.SplitN(render.StageLines(w), "\n", 2)[0], "\n")
			for _, s := range w.Stages {
				row := stageRowView{Number: itoa(s.Index + 1), Paths: render.Big(s.Count), Branches: itoa(s.BranchesTaken) + "/" + itoa(s.Branches)}
				if s.Big {
					row.Covered, row.Pct, row.Note = "-", "-", "too many paths to list: branch coverage only"
				} else {
					row.Covered = itoa(s.Covered) + "/" + itoa(len(s.Paths)-s.CannotBeTested)
					row.Pct = render.Pct(coverage.Percent(s.Covered, len(s.Paths)-s.CannotBeTested), nil)
					var notes []string
					if s.CannotBeTested > 0 {
						notes = append(notes, itoa(s.CannotBeTested)+" cannot be tested")
					}
					if s.NeedsReview > 0 {
						notes = append(notes, itoa(s.NeedsReview)+" need review")
					}
					if s.CutOff {
						notes = append(notes, "more incoming states than the limit were cut off")
					}
					row.Note = strings.Join(notes, "; ")
				}
				wv.Stages = append(wv.Stages, row)
			}
			if len(w.Pruned) > 0 {
				for _, l := range strings.Split(render.StageLines(w), "\n") {
					if strings.HasPrefix(l, "Left out of the list") {
						wv.PrunedLine = l
					}
				}
			}
		}
		wv.Priority, _ = coverage.Priority(w.Covered, len(w.Paths))
		wv.Pct = render.Pct(coverage.Percent(w.Covered, len(w.Paths)), nil)
		wv.Width = coverage.Percent(w.Covered, len(w.Paths))
		v.Workflows = append(v.Workflows, wv)
		data.addWorkflow(w, in.Classification != nil)
	}
	if v.Glossary != nil {
		data.addAudit(v.Glossary.Audit)
	}
	v.Data = data.finish()
	for i, r := range in.Trend {
		t := trendView{Time: r.Time.UTC().Format("2006-01-02 15:04 UTC"), Covered: r.Covered, Paths: r.Paths,
			Percent: math.Round(coverage.Percent(r.Covered, r.Paths)*10) / 10, Pct: render.Pct(coverage.Percent(r.Covered, r.Paths), nil)}
		if i > 0 && r.Scope != in.Trend[i-1].Scope {
			t.ScopeChanged = "scope changed: " + scopeCounts(r)
		}
		v.Trend = append(v.Trend, t)
	}
	return v.render()
}

// buildCards fills the five summary cards and the bucket bar from the
// numbers coverage.Compute made; nothing is recomputed here.
func (v *view) buildCards(res coverage.Result, classified bool) {
	b := res.Breakdown
	pct := func(c, p int) string { return render.Pct(coverage.Percent(c, p), nil) }
	if classified {
		t := b.TestableCoverage()
		v.Cards = append(v.Cards, cardView{Key: "testable-coverage", Title: "Testable coverage", Value: pct(t.Covered, t.Paths),
			Sub: plural(t.Covered, "of", t.Paths, "testable path"), Covered: t.Covered, Total: t.Paths})
	}
	o := b.Overall()
	v.Cards = append(v.Cards,
		cardView{Key: "overall-coverage", Title: "Overall coverage", Value: pct(o.Covered, o.Paths), Sub: plural(o.Covered, "of", o.Paths, "path"), Covered: o.Covered, Total: o.Paths},
		cardView{Key: "paths", Title: "Paths", Value: itoa(res.Paths), Sub: "all listed paths", Total: res.Paths},
		cardView{Key: "covered", Title: "Covered", Value: itoa(res.Covered), Sub: "a test ran them", Covered: res.Covered, Total: res.Paths},
		cardView{Key: "missed", Title: "Missed", Value: itoa(res.Paths - res.Covered), Sub: "no test ran them", Total: res.Paths},
	)
	if classified {
		v.Explain = "Testable coverage counts only the paths a test can run. Overall coverage counts every path, including those that can never happen, so it is always the lower number."
		total := float64(res.Paths)
		for _, bk := range []struct {
			key, name string
			c         coverage.BucketCount
		}{
			{"testable", "Testable", b.Testable},
			{"cannot-be-tested", "Cannot be tested", b.CannotBeTested},
			{"needs-special-test", "Needs special test", b.NeedsSpecialTest},
			{"needs-review", "Needs review", b.NeedsReview},
		} {
			w := 0.0
			if total > 0 {
				w = float64(bk.c.Paths) / total * 100
			}
			v.Buckets = append(v.Buckets, bucketView{Key: bk.key, Name: bk.name, Paths: bk.c.Paths, Covered: bk.c.Covered, Width: w})
		}
	} else {
		v.Explain = "Coverage here is overall: covered paths out of every listed path. Switch classification on in .pathkitrc.json to also see which missed paths can never run."
	}
}

// Analysis renders an analyze run's page: the paths, and a note that
// nothing was measured.
func Analysis(in AnalysisInput) ([]byte, error) {
	v := view{Title: "PathKit analysis", ExcludedLine: strings.TrimSuffix(in.ExcludedHeading, ":"), Excluded: in.Excluded}
	if len(in.Excluded) == 0 {
		v.ExcludedLine = "No workflows excluded"
	}
	v.setHeader(in.Header)
	var capped []string
	for _, w := range in.Workflows {
		if w.Paths.Truncated {
			capped = append(capped, w.Name)
		}
	}
	if len(capped) > 0 {
		v.CapHeadline, v.CapAdvice = render.CapHeadline(capped), render.CapAdvice()
	}
	data := newPageData(false, false)
	total := 0
	for wi, w := range in.Workflows {
		wv := workflowView{Name: w.Name, File: filepath.ToSlash(w.File), AddedByConfig: w.AddedByConfig, Truncated: w.Paths.Truncated,
			Total: len(w.Paths.List), TotalPathsLine: render.TotalLine(w.Paths, model.DefaultMaxPaths), Open: wi == 0}
		total += len(w.Paths.List)
		v.Workflows = append(v.Workflows, wv)
		data.addAnalysisWorkflow(w)
	}
	v.TotalPaths = total
	v.Cards = []cardView{
		{Key: "workflows", Title: "Workflows", Value: itoa(len(in.Workflows)), Sub: "in scope"},
		{Key: "paths", Title: "Paths", Value: itoa(total), Sub: "listed"},
	}
	v.Data = data.finish()
	return v.render()
}

func (v *view) setHeader(h Header) {
	v.Generated = h.Generated.UTC().Format("2006-01-02 15:04 UTC")
	v.Folder = filepath.ToSlash(h.Folder)
	v.ConfigNote = "no config file (no scope in use)"
	if h.Config != "" {
		v.ConfigNote = "config " + filepath.ToSlash(h.Config)
	}
	v.Version = version.String()
}

// The script's own text is allowed by the Content-Security-Policy through
// its hash, so the hash is computed from what the page really contains.
var (
	scriptBody      = regexp.MustCompile(`(?s)<script id="pk-code">(.*?)</script>`)
	hashPlaceholder = []byte("PKSCRIPTHASH")
)

func (v view) render() ([]byte, error) {
	var b bytes.Buffer
	if err := page.Execute(&b, v); err != nil {
		return nil, err
	}
	out := b.Bytes()
	if m := scriptBody.FindSubmatch(out); m != nil {
		sum := sha256.Sum256(m[1])
		out = bytes.Replace(out, hashPlaceholder, []byte(base64.StdEncoding.EncodeToString(sum[:])), 1)
	}
	return out, nil
}
