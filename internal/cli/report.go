package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type reportOptions struct {
	measure measureOptions
	out     string
	json    bool
	summary bool
	noColor bool
	clean   bool
	html    string
	hints   bool
	// unmatched adds the unmatched-trace report (--unmatched).
	unmatched bool
	// groups adds the missed paths grouped by first untested step (--groups).
	groups bool
}

func newReportCommand() *cobra.Command {
	var opt reportOptions
	cmd := &cobra.Command{
		Use:     "report [folder|folder/...]",
		Short:   "Show path coverage for every workflow in a project",
		Example: "  pathkit report\n  pathkit report --hints --html=report.html",
		Long: `Shows every in-scope workflow's path coverage, a priority label
(High below 50%, Medium 50-80%, Low above 80%), the project total, the
workflows excluded by the scope with their reasons, and what happened to
every trace file.

The numbers are exactly the ones "pathkit coverage" computes. With no
folder, report uses every entry of the config's "packages", else ./... .
Coverage is recorded only by "pathkit test" (plain "go test" records
nothing).

Exit codes: 0 fine, 1 a real error (including a package that doesn't
compile, or an in-scope workflow PathKit can't analyze), 2 coverage
below --fail-under, or (with classification enabled) the CI check failed:
testable coverage below classification.ci.fail_under_testable, or a
testable path with no test or a needs-review path that is not in the
baseline file (--baseline), or a path that had a test in the baseline
and has none now (--allow-lost-tests lets that pass). --write-baseline
writes that file.`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runReport(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.StringVar(&opt.out, "out", "", "also write the report to this `file`, always as plain text (default: the config's \"out\")")
	f.StringVar(&opt.measure.coverageBasis, "coverage-basis", "", "which coverage --fail-under checks: `basis`: testable or overall (default: overall; testable when the config enables classification)")
	f.StringVar(&opt.measure.failUnder, "fail-under", "", "exit 2 when path coverage is below this `percent` (0-100)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text (default: the config's \"json\")")
	f.BoolVar(&opt.summary, "summary", false, "one line per workflow instead of every path")
	f.BoolVar(&opt.hints, "hints", false, "under every testable path no test ran, say what a test must make happen to run it")
	f.BoolVar(&opt.unmatched, "unmatched", false, "add a report of the traces that fit no path: where each left the map, what the map expected, and the likely cause")
	f.BoolVar(&opt.groups, "groups", false, "add the testable paths no test ran, grouped by the first step no test took (biggest group first, with where it is and how to cover it)")
	f.BoolVar(&opt.noColor, "no-color", false, "never colour the output (also: the config's \"noColor\", or the NO_COLOR environment variable)")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.clean, "clean", false, "delete the trace files after the report (only when the report was produced)")
	addHTMLFlag(cmd, &opt.html, defaultReportHTML, "the report (both tabs)")
	addCIFlags(cmd, &opt.measure.ci, true)
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

func runReport(cmd *cobra.Command, target string, opt reportOptions) error {
	if err := htmlSpaceHint(cmd, opt.html, defaultReportHTML, target); err != nil {
		return err
	}
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	m, err := measure(cmd, "report", target, opt.measure)
	if err != nil {
		return err
	}

	// A flag always beats the config (including --json=false).
	cfg, fl := m.sc.cfg, cmd.Flags()
	asJSON, noColor, outFile := opt.json, opt.noColor, opt.out
	if cfg != nil {
		if !fl.Changed("json") {
			asJSON = cfg.JSON
		}
		if !fl.Changed("no-color") {
			noColor = cfg.NoColor
		}
		if !fl.Changed("out") && cfg.Out != "" {
			outFile = cfg.Abs(cfg.Out)
		}
	}
	by := m.excludedBy
	var html func() error
	if path := htmlPath(cmd, opt.html, defaultReportHTML, cfg); path != "" {
		html = reportHTML(cmd, m, path)
	}

	if asJSON {
		if opt.summary {
			fmt.Fprintln(cmd.ErrOrStderr(), "pathkit report: --summary ignored because --json was passed.")
		}
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // keep "->" readable in the path text
		enc.SetIndent("", "  ")
		out := reportJSON(m.res, m.threshold, by, m.basisInfo())
		out.CI = m.ci.json(m.res)
		if opt.hints {
			ws := make([]jsonWorkflow, len(out.Workflows))
			for i := range out.Workflows {
				ws[i] = out.Workflows[i].jsonWorkflow
			}
			addHints(ws, m.res)
			for i := range out.Workflows {
				out.Workflows[i].jsonWorkflow = ws[i]
			}
		}
		if opt.unmatched {
			out.Unmatched = unmatchedJSON(m.res)
		}
		if opt.groups {
			out.MissedGroups, out.MissedGroupsTotal = groupsJSON(m.res)
		}
		if err := enc.Encode(out); err != nil {
			return err
		}
		return finish(cmd, "report", m, buf.String(), buf.String(), outFile, opt.clean, html)
	}
	if opt.summary && opt.hints {
		fmt.Fprintln(cmd.ErrOrStderr(), "pathkit report: --hints ignored because --summary was passed (it lists no paths).")
	}
	ro := render.ReportOptions{Summary: opt.summary, Threshold: m.overallThreshold(), ExcludedBy: by, Classification: m.classView(), Hints: opt.hints}
	plain := render.ReportText(m.res, ro)
	printed := plain
	if render.ColorEnabled(noColor, os.Getenv("NO_COLOR"), isTerminal(cmd.OutOrStdout())) {
		ro.Color = true
		printed = render.ReportText(m.res, ro)
	}
	if opt.unmatched {
		plain += "\n" + render.UnmatchedSection(m.res)
		printed += "\n" + render.UnmatchedSection(m.res)
	}
	if opt.groups {
		sec := render.GroupsText(m.res)
		if sec == "" {
			sec = "Missed paths by first untested step: none (every testable path has a test).\n"
		}
		plain += "\n" + sec
		printed += "\n" + sec
	}
	return finish(cmd, "report", m, printed, plain, outFile, opt.clean, html)
}

// isTerminal reports whether w is a real terminal (not a pipe, a file or
// a test buffer). No extra dependency: a terminal is a character device.
//
// A variable, so a test can pretend stdout is a terminal.
var isTerminal = func(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// The report --json shape (schemaVersion 1), documented in SETUP-GUIDE.md:
// exactly the coverage --json shape, plus "file" and "priority" on each
// workflow and "excludedBy" at the top.
type (
	jsonReportWorkflow struct {
		jsonWorkflow
		File     string  `json:"file"`
		Priority *string `json:"priority"` // null: the workflow has no listed paths
	}
	jsonReport struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Tool          string               `json:"tool"`
		Workflows     []jsonReportWorkflow `json:"workflows"`
		Excluded      []jsonExcluded       `json:"excluded"`
		ExcludedBy    []string             `json:"excludedBy"`
		Total         jsonCount            `json:"total"`
		Branches      jsonCount            `json:"branches"`
		Traces        jsonTraces           `json:"traces"`
		FailUnder     *jsonFailUnder       `json:"failUnder"`

		Classification      jsonClassification `json:"classification"`
		CoverageBasis       string             `json:"coverageBasis"`
		ClassificationShown bool               `json:"classificationShown"`
		CI                  *jsonCI            `json:"ci,omitempty"`
		Unmatched           *[]jsonUnmatched   `json:"unmatchedTraces,omitempty"`
		// Absent when no workflow reached the path cap.
		PathCap *jsonPathCap `json:"pathCapWarning,omitempty"`
		// Assumptions that are off, or that claims rest on; absent when there is
		// nothing to say (the same as in the coverage JSON).
		Assumptions     []jsonAssumption `json:"assumptions,omitempty"`
		AssumptionPanel []jsonAssumption `json:"assumptionPanel,omitempty"`
		// Added with --groups only.
		MissedGroups      []jsonGroup `json:"missedGroups,omitempty"`
		MissedGroupsTotal int         `json:"missedGroupsTotal,omitempty"`
		// Added with the known-defect label; absent when no path has it.
		BlockedByKnownBugs int `json:"blockedByKnownBugs,omitempty"`
	}
	jsonGroup struct {
		Workflow string   `json:"workflow"`
		Paths    int      `json:"paths"`
		Step     string   `json:"firstUntestedStep"`
		File     string   `json:"file,omitempty"`
		Line     int      `json:"line,omitempty"`
		After    []string `json:"reachedAfter"`
		// NoTests: no test ran any path of the workflow.
		NoTests bool   `json:"noTests,omitempty"`
		Hint    string `json:"howToCover,omitempty"`
	}
)

// maxGroupsJSON is how many groups --groups --json lists; missedGroupsTotal
// says how many there are when that is more.
const maxGroupsJSON = 500

func groupsJSON(res coverage.Result) ([]jsonGroup, int) {
	gs := render.Groups(res)
	total := 0
	if len(gs) > maxGroupsJSON {
		total, gs = len(gs), gs[:maxGroupsJSON]
	}
	out := []jsonGroup{}
	for _, g := range gs {
		after := g.After
		if after == nil {
			after = []string{}
		}
		out = append(out, jsonGroup{Workflow: g.Workflow, Paths: g.Missed, Step: g.Step, File: g.File, Line: g.Line, After: after, NoTests: g.NoTests, Hint: g.Hint})
	}
	return out, total
}

func reportJSON(res coverage.Result, threshold *float64, by []string, bi basisInfo) jsonReport {
	c := coverageJSON(res, threshold, bi)
	out := jsonReport{SchemaVersion: c.SchemaVersion, Tool: c.Tool, Workflows: []jsonReportWorkflow{}, Excluded: c.Excluded,
		ExcludedBy: []string{}, Total: c.Total, Branches: c.Branches, Traces: c.Traces, FailUnder: c.FailUnder,
		Classification: c.Classification, CoverageBasis: c.CoverageBasis, ClassificationShown: c.ClassificationShown, PathCap: c.PathCap, Assumptions: c.Assumptions, AssumptionPanel: c.AssumptionPanel, BlockedByKnownBugs: c.BlockedByKnownBugs}
	out.ExcludedBy = append(out.ExcludedBy, by...)
	for i, w := range c.Workflows {
		// Forward slashes on every system, so the JSON is the same everywhere;
		// the text output uses the system's own separator.
		rw := jsonReportWorkflow{jsonWorkflow: w, File: filepath.ToSlash(res.Workflows[i].File)}
		if label, ok := coverage.Priority(res.Workflows[i].Covered, len(res.Workflows[i].Paths)); ok {
			rw.Priority = &label
		}
		out.Workflows = append(out.Workflows, rw)
	}
	return out
}
