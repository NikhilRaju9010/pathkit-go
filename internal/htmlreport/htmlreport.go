// Package htmlreport writes PathKit's shareable HTML report (CLAUDE.md
// D12): one self-contained file with a Coverage tab and an Analysis tab.
//
// Rules:
//   - It only lays out numbers and sentences computed elsewhere: coverage
//     results come from the shared measure (coverage.Compute) and every
//     sentence from package render, exactly as the terminal prints it.
//   - It uses html/template, which escapes every inserted value for where
//     it appears. No value is ever marked "safe" (template.HTML and its
//     relatives are never used; a test scans the code for them). The
//     stylesheet is part of the template text itself, not a value.
//   - It loads nothing: no scripts at all, no fonts, stylesheets or images
//     from anywhere, and a Content-Security-Policy that makes the browser
//     refuse any network request the page might attempt.
package htmlreport

import (
	"bytes"
	_ "embed"
	"html/template"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

var (
	//go:embed page.html.tmpl
	pageTemplate string
	//go:embed style.css
	style string

	page = template.Must(template.New("page").Parse(strings.Replace(pageTemplate, "/*STYLE*/", style, 1)))
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
		Title, Generated, Folder, ConfigNote string
		CoverageFirst, Measured              bool
		TotalPaths, TotalCovered             int
		TotalLine, BranchesLine, TracesLine  string
		FailUnder                            *failUnderView
		Workflows                            []workflowView
		ExcludedLine                         string
		Excluded                             []scope.Excluded
		Traces                               *coverage.Counts
		Trend                                []trendView
	}
	workflowView struct {
		Name, File, Stats, TotalPathsLine, Priority string
		AddedByConfig, Truncated                    bool
		Covered, Total                              int
		Paths                                       []pathView
	}
	pathView struct {
		Number         int
		Text           string
		Covered, Stale bool
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

// Report renders a report run's page: Coverage tab first.
func Report(in ReportInput) ([]byte, error) {
	res := in.Result
	v := view{
		Title: "PathKit report", CoverageFirst: true, Measured: true,
		TotalPaths: res.Paths, TotalCovered: res.Covered,
		TotalLine:    render.ProjectTotalLine(res, in.Threshold),
		BranchesLine: render.BranchesLine(res),
		TracesLine:   strings.TrimSuffix(render.TracesLine(res.Counts), "\n"),
		ExcludedLine: strings.TrimSuffix(render.ExcludedLine(len(res.Excluded), in.ExcludedBy), ":"),
		Excluded:     res.Excluded,
		Traces:       &res.Counts,
	}
	v.setHeader(in.Header)
	if in.Threshold != nil {
		pct := coverage.Percent(res.Covered, res.Paths)
		f := &failUnderView{Passed: pct >= *in.Threshold}
		if f.Passed {
			f.Text = "--fail-under " + render.Threshold(*in.Threshold) + ": passed (" + render.Pct(pct, in.Threshold) + ")"
		} else {
			f.Text = "--fail-under " + render.Threshold(*in.Threshold) + ": coverage " + render.Pct(pct, in.Threshold) + " is below it (exit code 2)"
		}
		v.FailUnder = f
	}
	for _, w := range res.Workflows {
		wv := workflowView{
			Name: w.Name, File: filepath.ToSlash(w.File), AddedByConfig: w.AddedByConfig, Truncated: w.Truncated,
			Stats: render.WorkflowStats(w), Covered: w.Covered, Total: len(w.Paths),
			TotalPathsLine: render.TotalLine(model.PathList{List: make([]model.Path, len(w.Paths)), Truncated: w.Truncated}, model.DefaultMaxPaths),
		}
		wv.Priority, _ = coverage.Priority(w.Covered, len(w.Paths))
		for _, p := range w.Paths {
			wv.Paths = append(wv.Paths, pathView{Number: p.Number, Text: render.PathText(p.Path), Covered: p.Covered, Stale: p.StaleTrace})
		}
		v.Workflows = append(v.Workflows, wv)
	}
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

// Analysis renders an analyze run's page: Analysis tab first, and a
// Coverage tab that says it wasn't measured.
func Analysis(in AnalysisInput) ([]byte, error) {
	v := view{Title: "PathKit analysis", ExcludedLine: strings.TrimSuffix(in.ExcludedHeading, ":"), Excluded: in.Excluded}
	if len(in.Excluded) == 0 {
		v.ExcludedLine = "No workflows excluded"
	}
	v.setHeader(in.Header)
	for _, w := range in.Workflows {
		wv := workflowView{Name: w.Name, File: filepath.ToSlash(w.File), AddedByConfig: w.AddedByConfig, Truncated: w.Paths.Truncated,
			Total: len(w.Paths.List), TotalPathsLine: render.TotalLine(w.Paths, model.DefaultMaxPaths)}
		for i, p := range w.Paths.List {
			wv.Paths = append(wv.Paths, pathView{Number: i + 1, Text: render.PathText(p)})
		}
		v.Workflows = append(v.Workflows, wv)
	}
	return v.render()
}

func (v *view) setHeader(h Header) {
	v.Generated = h.Generated.UTC().Format("2006-01-02 15:04 UTC")
	v.Folder = filepath.ToSlash(h.Folder)
	v.ConfigNote = "no config file (no scope in use)"
	if h.Config != "" {
		v.ConfigNote = "config " + filepath.ToSlash(h.Config)
	}
}

func (v view) render() ([]byte, error) {
	var b bytes.Buffer
	if err := page.Execute(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
