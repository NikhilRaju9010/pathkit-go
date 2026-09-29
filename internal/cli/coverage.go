package cli

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type coverageOptions struct {
	measure measureOptions
	out     string
	json    bool
	clean   bool
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
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (default: the config's \"traces\", else .pathkit/traces)")
	f.StringVar(&opt.measure.function, "function", "", "measure only this `workflow`")
	f.StringVar(&opt.out, "out", "", "also write exactly what was printed to this `file`")
	f.StringVar(&opt.measure.failUnder, "fail-under", "", "exit 2 when path coverage is below this `percent` (0-100)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.clean, "clean", false, "delete the trace files after the report (only when the report was produced)")
	addScopeFlags(cmd, &opt.measure.scope, false)
	return cmd
}

func runCoverage(cmd *cobra.Command, target string, opt coverageOptions) error {
	m, err := measure(cmd, "coverage", target, opt.measure)
	if err != nil {
		return err
	}
	var output string
	if opt.json {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // keep "->" readable in the path text
		enc.SetIndent("", "  ")
		if err := enc.Encode(coverageJSON(m.res, m.threshold)); err != nil {
			return err
		}
		output = buf.String()
	} else {
		output = render.CoverageText(m.res, m.threshold) + excludedBlock(m.res.Excluded) + "\n" + render.TracesLine(m.res.Counts)
	}
	return finish(cmd, "coverage", m, output, output, opt.out, opt.clean)
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
