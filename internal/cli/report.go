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
}

func newReportCommand() *cobra.Command {
	var opt reportOptions
	cmd := &cobra.Command{
		Use:   "report [folder|folder/...]",
		Short: "Show path coverage for every workflow in a project",
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
below --fail-under.`,
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
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (default: the config's \"traces\", else .pathkit/traces)")
	f.StringVar(&opt.out, "out", "", "also write the report to this `file`, always as plain text (default: the config's \"out\")")
	f.StringVar(&opt.measure.failUnder, "fail-under", "", "exit 2 when path coverage is below this `percent` (0-100)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text (default: the config's \"json\")")
	f.BoolVar(&opt.summary, "summary", false, "one line per workflow instead of every path")
	f.BoolVar(&opt.noColor, "no-color", false, "never colour the output (also: the config's \"noColor\", or the NO_COLOR environment variable)")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.clean, "clean", false, "delete the trace files after the report (only when the report was produced)")
	addHTMLFlag(cmd, &opt.html, defaultReportHTML, "the report (both tabs)")
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
		if err := enc.Encode(reportJSON(m.res, m.threshold, by)); err != nil {
			return err
		}
		return finish(cmd, "report", m, buf.String(), buf.String(), outFile, opt.clean, html)
	}
	ro := render.ReportOptions{Summary: opt.summary, Threshold: m.threshold, ExcludedBy: by}
	plain := render.ReportText(m.res, ro)
	printed := plain
	if render.ColorEnabled(noColor, os.Getenv("NO_COLOR"), isTerminal(cmd.OutOrStdout())) {
		ro.Color = true
		printed = render.ReportText(m.res, ro)
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
	}
)

func reportJSON(res coverage.Result, threshold *float64, by []string) jsonReport {
	c := coverageJSON(res, threshold)
	out := jsonReport{SchemaVersion: c.SchemaVersion, Tool: c.Tool, Workflows: []jsonReportWorkflow{}, Excluded: c.Excluded,
		ExcludedBy: []string{}, Total: c.Total, Branches: c.Branches, Traces: c.Traces, FailUnder: c.FailUnder}
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
