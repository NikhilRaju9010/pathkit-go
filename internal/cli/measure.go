package cli

import (
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

// measureOptions are the flags that decide what gets measured. coverage
// and report share them (M7: one code path, no second calculation).
type measureOptions struct {
	traces     string
	function   string // coverage only; "" measures every workflow in scope
	failUnder  string
	allowStale bool
	scope      scopeFlags
}

// measured is one measurement: the numbers, and what finish needs.
type measured struct {
	sc            *scoped
	res           coverage.Result
	traceDir      string
	tracePaths    []string // every trace file read (what --clean deletes)
	threshold     *float64 // nil: no --fail-under and no failUnder in the config
	thresholdFrom string
}

// measure is the only way coverage and report get their numbers. It loads
// target, applies the scope, refuses while an in-scope workflow can't be
// analyzed, reads the traces, computes coverage and prints each warning.
func measure(cmd *cobra.Command, cmdName, target string, opt measureOptions) (*measured, error) {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	m := &measured{thresholdFrom: "--fail-under"}

	if cmd.Flags().Changed("fail-under") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(opt.failUnder), "%"), 64)
		if err != nil || v < 0 || v > 100 || math.IsNaN(v) {
			return nil, userError("invalid --fail-under value: %q (it must be a number from 0 to 100)", opt.failUnder)
		}
		m.threshold = &v
	}

	sc, err := loadScoped(cmdName, target, opt.scope, stderr)
	if err != nil {
		return nil, err
	}
	m.sc = sc
	// Owner's decision (M5): the % must cover exactly what is in scope.
	if len(sc.notAnalyzable) > 0 {
		var parts []string
		for _, n := range sc.notAnalyzable {
			parts = append(parts, fmt.Sprintf("%s: %v", n.name, n.err))
		}
		return nil, userError("in scope but not analyzable: %s %s", strings.Join(parts, "; "), notAnalyzableHint)
	}
	if m.threshold == nil && sc.cfg != nil && sc.cfg.FailUnder != nil {
		m.threshold, m.thresholdFrom = sc.cfg.FailUnder, "failUnder in "+sc.cfg.Path
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
			return nil, userError("%s", err)
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
		return nil, userError("no workflows in scope to measure")
	}

	m.traceDir = traceDirFor(cmd, opt.traces, sc.cfg)
	m.tracePaths, err = trace.List(m.traceDir)
	if err != nil {
		return nil, userError("%s", err)
	}
	if len(m.tracePaths) == 0 {
		return nil, userError("%s", trace.NoTracesMessage(m.traceDir))
	}
	var files []coverage.TraceFile
	for _, p := range m.tracePaths {
		f, err := trace.Read(p)
		files = append(files, coverage.TraceFile{Path: p, File: f, ReadErr: err})
	}

	m.res = coverage.Compute(coverage.Input{Workflows: workflows, Excluded: sc.excluded, Others: others, Traces: files, AllowStale: allowStale})
	for _, w := range m.res.Warnings {
		fmt.Fprintf(stderr, "pathkit %s: warning: %s\n", cmdName, w.Message)
	}
	return m, nil
}

// finish is what coverage and report do once their output is built:
// print it, write --out, then --clean (only now that the report was fully
// produced: exit 0 or 2, never after an error, owner's condition M6), and
// last the --fail-under check (exit 2).
func finish(cmd *cobra.Command, cmdName string, m *measured, output, outFile string, clean bool) error {
	fmt.Fprint(cmd.OutOrStdout(), output)
	if outFile != "" {
		if err := os.WriteFile(outFile, []byte(output), 0o644); err != nil {
			return userError("could not write --out file: %v", err)
		}
	}
	if clean {
		deleted := 0
		for _, p := range m.tracePaths {
			if err := os.Remove(p); err == nil {
				deleted++
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "pathkit %s: deleted %s from %s\n", cmdName, count(deleted, "trace file"), m.traceDir)
	}
	if m.threshold != nil {
		if v := coverage.Percent(m.res.Covered, m.res.Paths); v < *m.threshold {
			return belowThresholdError("coverage %s is below %s %s", render.Pct(v, m.threshold), m.thresholdFrom, render.Threshold(*m.threshold))
		}
	}
	return nil
}
