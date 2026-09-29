package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

type coverageOptions struct {
	traces     string
	function   string
	out        string
	failUnder  string
	json       bool
	allowStale bool
	clean      bool
	scope      scopeFlags
}

func newCoverageCommand() *cobra.Command {
	var opt coverageOptions
	cmd := &cobra.Command{
		Use:   "coverage <file|folder|folder/...>",
		Short: "Show which paths your tests ran, workflow by workflow",
		Long: `Compares the traces "pathkit test" recorded with the paths "analyze"
lists, and shows which paths your tests ran.

Exit codes: 0 fine, 1 a real error, 2 coverage below --fail-under.
Traces that don't count (stale, unmatched, incomplete, ...) are warnings
on stderr; they never change the exit code.`,
		Args: exactlyOne("<file>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCoverage(cmd, args[0], opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.traces, "traces", defaultTraceDir, "folder the trace files are in (default: the config's \"traces\", else .pathkit/traces)")
	f.StringVar(&opt.function, "function", "", "measure only this `workflow`")
	f.StringVar(&opt.out, "out", "", "also write exactly what was printed to this `file`")
	f.StringVar(&opt.failUnder, "fail-under", "", "exit 2 when path coverage is below this `percent` (0-100)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.clean, "clean", false, "delete the trace files after the report (only when the report was produced)")
	addScopeFlags(cmd, &opt.scope, false)
	return cmd
}

func runCoverage(cmd *cobra.Command, target string, opt coverageOptions) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

	var threshold *float64
	thresholdFrom := "--fail-under"
	if cmd.Flags().Changed("fail-under") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(opt.failUnder), "%"), 64)
		if err != nil || v < 0 || v > 100 || math.IsNaN(v) {
			return userError("invalid --fail-under value: %q (it must be a number from 0 to 100)", opt.failUnder)
		}
		threshold = &v
	}

	sc, err := loadScoped("coverage", target, opt.scope, stderr)
	if err != nil {
		return err
	}
	// Owner's decision (M5): the % must cover exactly what is in scope.
	if len(sc.notAnalyzable) > 0 {
		var parts []string
		for _, n := range sc.notAnalyzable {
			parts = append(parts, fmt.Sprintf("%s: %v", n.name, n.err))
		}
		return userError("in scope but not analyzable: %s %s", strings.Join(parts, "; "), notAnalyzableHint)
	}
	if threshold == nil && sc.cfg != nil && sc.cfg.FailUnder != nil {
		threshold, thresholdFrom = sc.cfg.FailUnder, "failUnder in "+sc.cfg.Path
	}
	allowStale := opt.allowStale || (sc.cfg != nil && sc.cfg.AllowStale)

	var workflows []coverage.Workflow
	var others []string
	for _, r := range sc.mapped {
		workflows = append(workflows, coverage.Workflow{Name: r.wf.Name, Graph: r.graph, Hash: r.hash, AddedByConfig: r.addedByConfig})
	}
	if opt.function != "" {
		var names []string
		for _, w := range workflows {
			names = append(names, w.Name)
		}
		name, err := scope.MatchName(names, opt.function, "--function", target)
		if err != nil {
			return userError("%s", err)
		}
		var picked []coverage.Workflow
		for _, w := range workflows {
			if w.Name == name {
				picked = append(picked, w)
			} else {
				others = append(others, w.Name)
			}
		}
		workflows = picked
	}
	if len(workflows) == 0 {
		fmt.Fprint(stdout, strings.TrimPrefix(excludedBlock(sc.excluded), "\n"))
		return userError("no workflows in scope to measure")
	}

	traceDir := traceDirFor(cmd, opt.traces, sc.cfg)
	paths, err := trace.List(traceDir)
	if err != nil {
		return userError("%s", err)
	}
	if len(paths) == 0 {
		return userError("%s", trace.NoTracesMessage(traceDir))
	}
	var files []coverage.TraceFile
	for _, p := range paths {
		f, err := trace.Read(p)
		files = append(files, coverage.TraceFile{Path: p, File: f, ReadErr: err})
	}

	res := coverage.Compute(coverage.Input{Workflows: workflows, Excluded: sc.excluded, Others: others, Traces: files, AllowStale: allowStale})
	for _, w := range res.Warnings {
		fmt.Fprintf(stderr, "pathkit coverage: warning: %s\n", w.Message)
	}

	var output string
	if opt.json {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // keep "->" readable in the path text
		enc.SetIndent("", "  ")
		if err := enc.Encode(coverageJSON(res, threshold)); err != nil {
			return err
		}
		output = buf.String()
	} else {
		output = render.CoverageText(res, threshold) + excludedBlock(res.Excluded) + "\n" + render.TracesLine(res.Counts)
	}
	fmt.Fprint(stdout, output)
	if opt.out != "" {
		if err := os.WriteFile(opt.out, []byte(output), 0o644); err != nil {
			return userError("could not write --out file: %v", err)
		}
	}

	// The report is fully produced; only now may --clean delete traces
	// (exit 0 or 2, never after an error: owner's condition, M6).
	if opt.clean {
		deleted := 0
		for _, p := range paths {
			if err := os.Remove(p); err == nil {
				deleted++
			}
		}
		fmt.Fprintf(stderr, "pathkit coverage: deleted %s from %s\n", count(deleted, "trace file"), traceDir)
	}

	if threshold != nil {
		if v := coverage.Percent(res.Covered, res.Paths); v < *threshold {
			return belowThresholdError("coverage %s is below %s %s", render.Pct(v, threshold), thresholdFrom, render.Threshold(*threshold))
		}
	}
	return nil
}

// The --json shape (schemaVersion 1), documented in SETUP-GUIDE.md.
type (
	jsonCount struct {
		Total   int     `json:"total"`
		Covered int     `json:"covered"`
		Percent float64 `json:"percent"` // one decimal; use covered/total for exact math
	}
	jsonPath struct {
		Number       int      `json:"number"`
		ID           string   `json:"id"`
		Text         string   `json:"text"`
		Steps        []string `json:"steps"`
		End          string   `json:"end"`
		Compensation bool     `json:"compensation"`
		Covered      bool     `json:"covered"`
		Traces       int      `json:"traces"`
		StaleTrace   bool     `json:"staleTrace"`
	}
	jsonWorkflow struct {
		Name          string     `json:"name"`
		AddedByConfig bool       `json:"addedByConfig"`
		Paths         jsonCount  `json:"paths"`
		Branches      jsonCount  `json:"branches"`
		Truncated     bool       `json:"truncated"`
		PathList      []jsonPath `json:"pathList"`
	}
	jsonExcluded struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	jsonTraces struct {
		Read       int `json:"read"`
		Counted    int `json:"counted"`
		Unmatched  int `json:"unmatched"`
		Stale      int `json:"stale"`
		Incomplete int `json:"incomplete"`
		Excluded   int `json:"excluded"`
		Unknown    int `json:"unknownWorkflow"`
		Unreadable int `json:"unreadable"`
		Other      int `json:"otherWorkflows"`
	}
	jsonFailUnder struct {
		Threshold float64 `json:"threshold"`
		Passed    bool    `json:"passed"`
	}
	jsonCoverage struct {
		SchemaVersion int            `json:"schemaVersion"`
		Tool          string         `json:"tool"`
		Workflows     []jsonWorkflow `json:"workflows"`
		Excluded      []jsonExcluded `json:"excluded"`
		Total         jsonCount      `json:"total"`
		Branches      jsonCount      `json:"branches"`
		Traces        jsonTraces     `json:"traces"`
		FailUnder     *jsonFailUnder `json:"failUnder"`
	}
)

func count1(covered, total int) jsonCount {
	return jsonCount{Total: total, Covered: covered, Percent: math.Round(coverage.Percent(covered, total)*10) / 10}
}

func coverageJSON(res coverage.Result, threshold *float64) jsonCoverage {
	out := jsonCoverage{
		SchemaVersion: 1, Tool: "pathkit-go",
		Workflows: []jsonWorkflow{}, Excluded: []jsonExcluded{},
		Total:    count1(res.Covered, res.Paths),
		Branches: count1(res.BranchesTaken, res.Branches),
		Traces: jsonTraces{Read: res.Counts.Read, Counted: res.Counts.Counted, Unmatched: res.Counts.Unmatched, Stale: res.Counts.Stale,
			Incomplete: res.Counts.Incomplete, Excluded: res.Counts.Excluded, Unknown: res.Counts.Unknown, Unreadable: res.Counts.Unreadable, Other: res.Counts.Other},
	}
	for _, w := range res.Workflows {
		jw := jsonWorkflow{Name: w.Name, AddedByConfig: w.AddedByConfig, Paths: count1(w.Covered, len(w.Paths)),
			Branches: count1(w.BranchesTaken, w.Branches), Truncated: w.Truncated, PathList: []jsonPath{}}
		for _, p := range w.Paths {
			steps := []string{}
			for _, s := range p.Path.Steps {
				steps = append(steps, s.Exit.ID.String())
			}
			jw.PathList = append(jw.PathList, jsonPath{Number: p.Number, ID: p.Path.ID(), Text: render.PathText(p.Path), Steps: steps,
				End: string(p.Path.End), Compensation: p.Path.Compensation, Covered: p.Covered, Traces: p.Traces, StaleTrace: p.StaleTrace})
		}
		out.Workflows = append(out.Workflows, jw)
	}
	for _, e := range res.Excluded {
		out.Excluded = append(out.Excluded, jsonExcluded{e.Name, e.Reason})
	}
	if threshold != nil {
		out.FailUnder = &jsonFailUnder{Threshold: *threshold, Passed: coverage.Percent(res.Covered, res.Paths) >= *threshold}
	}
	return out
}
