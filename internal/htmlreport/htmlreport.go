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
	"fmt"
	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
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

	page = template.Must(template.New("page").Funcs(template.FuncMap{"tipFor": tipFor}).Parse(strings.Replace(strings.Replace(pageTemplate, "/*STYLE*/", style, 1), "/*SCRIPT*/", script, 1)))
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
	// Panel is every assumption, on or off, with its claim count; the summary
	// shows it.
	Panel []classify.AssumptionStatus
	// TopTests are the tests to write first (advice.TestsToWriteFirst).
	TopTests []advice.Item
	// Outcomes are the missed decision outcomes, riskiest first (advice.MissedOutcomes).
	Outcomes []advice.OutcomeItem
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
		// HeadlineLines replace TotalLine and BranchesLine when a workflow has
		// more paths than the list holds (coverage.Headline).
		HeadlineLines []string
		// MainHeadline are the first lines of the summary: workflows meeting their
		// target and branch coverage; MainRows is the table of main numbers.
		MainHeadline []string
		MainRows     []mainRowView
		// Tests is what each Go test covered; TestsLine says it in a sentence.
		Tests     []testRowView
		TestsLine string
		// Advice is the tests to write first, the first section of the missed area.
		Advice       []adviceRowView
		Explain      string // what "testable" and "overall" mean
		Cards        []cardView
		Buckets      []bucketView
		ClassLines   []string
		FailUnder    *failUnderView
		Workflows    []workflowView
		ExcludedLine string
		Excluded     []scope.Excluded
		Traces       *coverage.Counts
		Trend        []trendView
		// TrendChart is the inline SVG of the headline over the last runs.
		TrendChart *chartView
		Glossary   *glossaryView
		// Groups are the testable paths no test ran, grouped by the first step
		// no test took (biggest first, at most maxGroupsHTML); GroupsMore is
		// how many more groups there are.
		Panel []panelRow
		// Unread are the places PathKit could not fully read (every one);
		// UnreadNone is the line shown when there are none.
		Unread     []unreadRowView
		UnreadNone string
		// Handlers is the handler table: signals, queries, updates and update validators.
		Handlers []handlerRowView
		// CallOutcomes is the outcome table: the outcomes of every checked call
		// and whether a test saw them.
		CallOutcomes []callRowView
		// LoopRounds is the loop rounds table: which of 0, 1 and 2+ rounds each
		// loop can run and which a test ran.
		LoopRounds              []loopRowView
		Groups                  []groupRowView
		GroupsPaths             int
		GroupsTotal, GroupsMore int
		Data                    *pageData
		ScriptHash              string
		// The part made for people who test (see qa.go). Hero is nil on the page
		// of "analyze", which has no tests to measure.
		Hero       *heroView
		SmallCards []smallCardView
		Bugs       []bugView
		Survivors  []bugView
		Next       []nextRowView
		NextMore   int
		WfRows     []wfRowView
		WfSections []wfSection
		AtTarget   string
		RunCount   int
		Terms      []term
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
		// Helpers are the summarized helpers (each mapped once, covered by any
		// call); HelpersNote says why they are summarized.
		Helpers     []helperRowView
		HelpersNote string
		PrunedLine  string
		// MainLines are the workflow's main number, why, and its scenarios.
		MainLines []string
	}
	adviceRowView struct {
		Rank                                 int
		Tag, Workflow, Where, Hint, Name, ID string
		Covers                               string
	}
	testRowView struct {
		Name, Workflows                                              string
		Traces, Scenarios, UniqueScenarios, Branches, UniqueBranches int
		AddsNothing                                                  bool
	}
	mainRowView struct {
		Name, Number, Target, Why, Scenarios string
		Met, Measured                        bool
	}
	panelRow struct {
		Name, Status, Claims string
		On                   bool
	}
	loopRowView struct {
		Where, Loop, Workflow string
		Rounds                []loopRoundView
		Gap                   bool
	}
	loopRoundView struct {
		Bucket string
		State  string // "seen", "not seen" or "cannot happen"
	}
	callRowView struct {
		Where, Call, Workflow string
		Outcomes              []callOutcomeView
		Gap                   bool
	}
	callOutcomeView struct {
		Name string
		Seen bool
	}
	handlerRowView struct {
		Kind, Name, Owner, Where, Exercised, Branches string
		Gap                                           bool
	}
	unreadRowView struct {
		Workflow, Where, Kind, Call, Effect, Suggestion string
	}
	groupRowView struct {
		Workflow, Step, Where, After, Hint string
		Paths                              int
	}
	helperRowView struct {
		Name, Where, Branches, Sites string
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
	v.buildPeople(res, in.Outcomes)
	v.MainHeadline = render.MainHeadlineLines(res)
	for _, t := range res.Tests {
		v.Tests = append(v.Tests, testRowView{Name: t.Name, Workflows: strings.Join(t.Workflows, ", "), Traces: t.Traces, Scenarios: t.Scenarios, UniqueScenarios: t.UniqueScenarios, Branches: t.Branches, UniqueBranches: t.UniqueBranches, AddsNothing: t.AddsNothing})
	}
	v.TestsLine = render.TestsLine(res)
	for i, it := range in.TopTests {
		where := it.File
		if it.Line > 0 {
			where = fmt.Sprintf("%s:%d", it.File, it.Line)
		}
		v.Advice = append(v.Advice, adviceRowView{Rank: i + 1, Tag: it.Tag, Workflow: it.Workflow, Where: where, Hint: it.Hint, Name: it.TestName, ID: it.ID, Covers: it.Covers()})
	}
	for _, w := range res.Workflows {
		m := w.Main
		row := mainRowView{Name: w.Name, Why: m.Why, Met: m.Met, Measured: m.Measured, Target: render.Threshold(m.Target), Scenarios: render.ScenarioLine(w)}
		if m.Measured {
			row.Number = fmt.Sprintf("%s %d/%d (%s)", mainKindName(m.Kind), m.Covered, m.Total, render.Pct(m.Percent, nil))
		} else {
			row.Number = "not measured"
		}
		v.MainRows = append(v.MainRows, row)
	}
	if _, capped := res.Headline(); capped {
		v.HeadlineLines = render.HeadlineLines(res, overallThreshold(in), in.Classification != nil)
	}
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
		shown := in.Assumptions
		if len(in.Panel) > 0 {
			shown = in.Panel // the glossary defines every assumption the summary lists
		}
		v.Glossary = buildGlossary(res, in.CustomLabels, shown)
	}
	v.buildGroups(res)
	for _, h := range res.Handlers {
		row := handlerRowView{Kind: h.Kind, Name: render.HandlerName(h), Owner: h.Owner, Where: filepath.ToSlash(h.Where), Exercised: h.Exercised, Gap: h.IsGap(), Branches: "-"}
		if h.Workflow != "" {
			row.Branches = itoa(h.BranchesTaken) + "/" + itoa(h.Branches)
			if h.Branches == 0 {
				row.Branches = "no branches"
			}
		}
		v.Handlers = append(v.Handlers, row)
	}
	for _, r := range res.CallOutcomes {
		row := callRowView{Where: fmt.Sprintf("%s:%d", filepath.ToSlash(r.File), r.Line), Call: r.Name + " (" + r.Kind + ")", Workflow: r.Workflow, Gap: len(r.Gaps()) > 0}
		for _, o := range r.Outcomes {
			row.Outcomes = append(row.Outcomes, callOutcomeView{Name: o.Outcome, Seen: o.Seen})
		}
		v.CallOutcomes = append(v.CallOutcomes, row)
	}
	for _, r := range res.LoopRounds {
		row := loopRowView{Where: fmt.Sprintf("%s:%d", filepath.ToSlash(r.File), r.Line), Loop: r.Loop, Workflow: r.Workflow, Gap: len(r.Gaps()) > 0}
		for _, c := range r.Rounds {
			state := "seen"
			switch {
			case !c.Possible:
				state = "cannot happen"
			case !c.Seen:
				state = "not seen"
			}
			row.Rounds = append(row.Rounds, loopRoundView{Bucket: c.Bucket, State: state})
		}
		v.LoopRounds = append(v.LoopRounds, row)
	}
	v.UnreadNone = render.UnreadNone
	for _, u := range res.Unread {
		where := filepath.ToSlash(u.File)
		if u.Line > 0 {
			where = fmt.Sprintf("%s:%d", where, u.Line)
		}
		v.Unread = append(v.Unread, unreadRowView{Workflow: u.Workflow, Where: where, Kind: u.Kind, Call: u.Call, Effect: u.Effect, Suggestion: u.Suggestion})
	}
	for _, a := range in.Panel {
		status := "on"
		if !a.On {
			status = "OFF: " + a.Why
		}
		v.Panel = append(v.Panel, panelRow{Name: a.Name, Status: status, Claims: render.ClaimsText(a.Claims), On: a.On})
	}
	data := newPageData(true, in.Classification != nil)
	for wi, w := range res.Workflows {
		wv := workflowView{
			Name: w.Name, File: filepath.ToSlash(w.File), AddedByConfig: w.AddedByConfig, Truncated: w.Truncated,
			Stats: render.WorkflowStats(w), Covered: w.Covered, Total: len(w.Paths), Missed: len(w.Paths) - w.Covered,
			TotalPathsLine: render.TotalLine(model.PathList{List: make([]model.Path, len(w.Paths)), Truncated: w.Truncated}, model.DefaultMaxPaths),
			Open:           wi == 0,
		}
		wv.MainLines = render.MainBlock(w)
		if lines := render.HelperLines(w); len(lines) > 0 {
			wv.HelpersNote = lines[0]
			for _, u := range w.Units {
				row := helperRowView{Name: u.Name, Where: fmt.Sprintf("%s:%d", filepath.ToSlash(u.File), u.Line), Branches: itoa(u.BranchesTaken) + "/" + itoa(u.Branches), Sites: itoa(u.Sites)}
				if u.Branches == 0 {
					row.Branches = "no branches"
				}
				wv.Helpers = append(wv.Helpers, row)
			}
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
	v.TrendChart = TrendChart(in.Trend)
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
	pct := func(c, p int) string {
		if p == 0 {
			return "not measured"
		}
		return render.Pct(coverage.Percent(c, p), nil)
	}
	if h, capped := res.Headline(); capped {
		// A workflow over the cap: the listed-path numbers stay out of the cards.
		name := func(s string) string {
			if classified {
				return s + " (testable)"
			}
			return s
		}
		v.Cards = append(v.Cards,
			cardView{Key: "path-coverage", Title: name("Path coverage"), Value: pct(h.PathCovered, h.PathTotal), Sub: plural(h.PathCovered, "of", h.PathTotal, "path") + " in " + cnt(h.PathWorkflows, "workflow") + " under the cap", Covered: h.PathCovered, Total: h.PathTotal},
			cardView{Key: "stage-coverage", Title: name("Stage coverage"), Value: pct(h.StageCovered, h.StageTotal), Sub: plural(h.StageCovered, "of", h.StageTotal, "stage path") + " in " + cnt(h.TooBig, "workflow") + " too big for path coverage", Covered: h.StageCovered, Total: h.StageTotal},
			cardView{Key: "branch-coverage", Title: "Branch coverage", Value: pct(h.BranchCovered, h.BranchTotal), Sub: plural(h.BranchCovered, "of", h.BranchTotal, "branch") + " over the whole map", Covered: h.BranchCovered, Total: h.BranchTotal},
		)
		if classified {
			v.Explain = "Path coverage counts only the paths a test can run, in the workflows whose paths are all listed. A workflow with more paths than the list holds is measured by stage coverage and branch coverage instead. A stage is a stretch of the workflow between two decisions every path goes through."
			total := float64(res.Paths)
			for _, bk := range []struct {
				key, name string
				c         coverage.BucketCount
			}{{"testable", "Testable", b.Testable}, {"cannot-be-tested", "Cannot be tested", b.CannotBeTested}, {"needs-special-test", "Needs special test", b.NeedsSpecialTest}, {"needs-review", "Needs review", b.NeedsReview}} {
				w := 0.0
				if total > 0 {
					w = float64(bk.c.Paths) / total * 100
				}
				v.Buckets = append(v.Buckets, bucketView{Key: bk.key, Name: bk.name, Paths: bk.c.Paths, Covered: bk.c.Covered, Width: w})
			}
		} else {
			v.Explain = "Path coverage is over the workflows whose paths are all listed. A workflow with more paths than the list holds is measured by stage coverage and branch coverage instead."
		}
		return
	}
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

// maxGroupsHTML is how many missed-path groups the page lists.
const maxGroupsHTML = 100

func (v *view) buildGroups(res coverage.Result) {
	gs := render.Groups(res)
	v.GroupsTotal = len(gs)
	for _, g := range gs {
		v.GroupsPaths += g.Missed
	}
	if len(gs) > maxGroupsHTML {
		v.GroupsMore, gs = len(gs)-maxGroupsHTML, gs[:maxGroupsHTML]
	}
	for _, g := range gs {
		after := "the tests get this far: only the start of the workflow"
		if g.NoTests {
			after = "no test ran this workflow"
		} else if len(g.After) > 0 {
			after = "the tests get this far: " + render.AfterText(g.After)
		}
		v.Groups = append(v.Groups, groupRowView{Workflow: g.Workflow, Step: g.Step, Where: g.Where(), After: after, Hint: g.Hint, Paths: g.Missed})
	}
}

func cnt(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func mainKindName(kind string) string { return kind + " coverage" }
