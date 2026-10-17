package cli

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/baseline"
	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// measureOptions are the flags that decide what gets measured. coverage
// and report share them (one code path, no second calculation).
type measureOptions struct {
	traces    string
	function  string // coverage only; "" measures every workflow in scope
	failUnder string
	// coverageBasis is --coverage-basis: "testable" or "overall" (see
	// basisFor).
	coverageBasis string
	allowStale    bool
	scope         scopeFlags
	// foldersOnly (report): refuse a single .go file.
	foldersOnly bool
	// tracesOptional (explain): no trace files is not an error; every path
	// is then simply not covered.
	tracesOptional bool
	// noCI (explain): the CI check is not run (no baseline is read).
	noCI bool
	ci   ciOptions
}

// measured is one measurement: the numbers, and what finish needs.
type measured struct {
	sc            *scoped
	res           coverage.Result
	traceDir      string
	tracePaths    []string // every trace file read (what --clean deletes)
	threshold     *float64 // nil: no --fail-under and no failUnder in the config
	thresholdFrom string
	excludedBy    []string // what excluded workflows (render.ExcludedLine)
	// basis is what --fail-under measures: overall, or testable. showClass
	// is whether the classification block is printed (and the basis named
	// in the --fail-under message): when classification is enabled in the
	// config, or --coverage-basis was given. Otherwise PathKit prints
	// exactly what it printed before classification existed.
	basis      coverage.Basis
	showClass  bool
	classNotes []string // where the markings came from (classify.Engine.Notes)
	// assumptions that are off, or that claims rest on (classify.Engine.Assumptions)
	assumptions []classify.AssumptionStatus
	// panel is every assumption, on or off, with its claim count
	// (classify.Engine.AssumptionPanel); every report prints it.
	panel []classify.AssumptionStatus
	// customLabels are the config's custom label definitions (the HTML
	// glossary shows them next to the built-in ones).
	customLabels []classify.Definition
	// ci is the CI check (ci.go); nil when it is off.
	ci *ciState
	// engine is the classifier (nil when classification is off); explain
	// asks it what was claimed about a path a test ran anyway.
	engine *classify.Engine
}

// classView is what the classification block prints, or nil when it is off.
func (m *measured) classView() *render.ClassificationView {
	if !m.showClass {
		return nil
	}
	return &render.ClassificationView{Basis: m.basis, Threshold: m.threshold, Notes: m.classNotes, Panel: m.panel}
}

// overallThreshold is the --fail-under value for the overall figures
// (the project total line). When the testable basis is the one being
// checked, the overall figure is not what --fail-under compares, so it
// gets no threshold-aware extra decimals. Off (no classification), it is
// the plain threshold, so existing output does not change.
func (m *measured) overallThreshold() *float64 {
	if m.showClass && m.basis == coverage.BasisTestable {
		return nil
	}
	return m.threshold
}

// basisFor picks the coverage basis. --coverage-basis wins. Without it,
// the basis is testable when classification is enabled in the config, and
// overall otherwise, so --fail-under works exactly as it did before.
//
// measure calls it twice: first with a nil cfg, only to reject a bad
// --coverage-basis value early (the config does not matter for that),
// then with the real config.
func basisFor(cmd *cobra.Command, flagValue string, cfg *scope.Config) (basis coverage.Basis, show bool, err error) {
	enabled := cfg != nil && cfg.Classification.Enabled
	if cmd.Flags().Changed("coverage-basis") {
		switch flagValue {
		case string(coverage.BasisTestable), string(coverage.BasisOverall):
			return coverage.Basis(flagValue), true, nil
		}
		return "", false, userError("invalid --coverage-basis value: %q (use testable or overall)", flagValue)
	}
	if enabled {
		return coverage.BasisTestable, true, nil
	}
	return coverage.BasisOverall, false, nil
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

	if cmd.Flags().Changed("coverage-basis") {
		// Checked early, like --fail-under, so a typo is reported before any slow work.
		if _, _, err := basisFor(cmd, opt.coverageBasis, nil); err != nil {
			return nil, err
		}
	}

	if opt.foldersOnly && target != "" {
		if _, _, file, err := load.Resolve(target); err != nil {
			return nil, userError("%s", err)
		} else if file != "" {
			return nil, userError("expected a folder or folder/..., not a file: %s (%s covers a whole project; use \"pathkit coverage\" for one file)", target, cmdName)
		}
	}

	opt.scope.wantNames = true
	sc, err := loadScoped(cmdName, target, opt.scope, stderr)
	if err != nil {
		return nil, err
	}
	m.sc = sc
	if m.basis, m.showClass, err = basisFor(cmd, opt.coverageBasis, sc.cfg); err != nil {
		return nil, err
	}
	if !opt.noCI {
		if m.ci, err = prepareCI(cmd, opt.ci, sc, opt.scope, opt.function); err != nil {
			return nil, err
		}
	}
	m.excludedBy = excludedBy(sc, opt.scope)
	// The % must cover exactly what is in scope.
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
		workflows = append(workflows, coverage.Workflow{Name: r.wf.Name, File: load.DisplayPath(r.wf.Filename), Graph: r.graph, Hash: r.hash, Package: r.wf.Pkg.PkgPath, AddedByConfig: r.addedByConfig})
	}
	if opt.function != "" {
		var names []string
		for _, w := range workflows {
			names = append(names, w.Name)
		}
		name, err := scope.MatchName(names, opt.function, "--function", sc.target)
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
		fmt.Fprint(stdout, render.ExcludedBlock(sc.excluded, m.excludedBy))
		return nil, userError("no workflows in scope to measure: the packages have no exported workflow functions (workflow.Context first, error last), or the scope (.pathkitrc.json, --include, --exclude) leaves none; the excluded ones are listed above")
	}

	m.traceDir = traceDirFor(cmd, opt.traces, sc.cfg)
	m.tracePaths, err = trace.List(m.traceDir)
	if err != nil {
		return nil, userError("%s", err)
	}
	if len(m.tracePaths) == 0 && !opt.tracesOptional {
		return nil, userError("%s", trace.NoTracesMessage(m.traceDir))
	}
	var files []coverage.TraceFile
	for _, p := range m.tracePaths {
		f, err := trace.Read(p)
		files = append(files, coverage.TraceFile{Path: p, File: f, ReadErr: err})
	}

	engine, err := classificationEngine(sc, cmdName, stderr)
	if err != nil {
		return nil, err
	}
	m.engine = engine
	in := coverage.Input{Workflows: workflows, Excluded: sc.excluded, Others: others, Traces: files, AllowStale: allowStale}
	if engine != nil {
		in.Classifier, in.Policy, in.Explained = engine.Classify, engine.Policy(), engine.Explained()
		in.Listing, in.Plan = engine.Listing, engine.PlanStages
	}
	m.res = coverage.Compute(in)
	if m.res.InternalError != nil {
		return nil, userError("%v", m.res.InternalError)
	}
	if m.ci != nil && engine != nil {
		m.ci.anchors = ruleAnchors(engine.Anchors())
	}
	if err := m.ci.compare(m.res); err != nil {
		return nil, err
	}
	if m.ci != nil {
		for _, w := range m.ci.anchorWarnings {
			fmt.Fprintf(stderr, "pathkit %s: warning: %s\n", cmdName, w)
		}
		if lw := m.ci.lostWarnings(m.res); len(lw) > 0 {
			fmt.Fprintf(stderr, "pathkit %s: warning: %s\n", cmdName, strings.Join(lw, "\n"))
		}
	}
	for _, w := range m.res.Warnings {
		fmt.Fprintf(stderr, "pathkit %s: warning: %s\n", cmdName, w.Message)
		if w.Entry != "" {
			fmt.Fprintf(stderr, "  to accept it after looking at it, add this to classification.explained_traces and replace the reason: %s\n", w.Entry)
		}
	}
	if names := render.ZeroPathNames(m.res); len(names) > 0 && cmdName != "explain" {
		fmt.Fprintln(stderr, "pathkit "+cmdName+": "+render.ZeroPathHeadline(names))
		for _, l := range render.ZeroPathAdvice() {
			fmt.Fprintln(stderr, "  "+l)
		}
	}
	if engine != nil {
		var measured []string
		for _, w := range workflows {
			measured = append(measured, w.Name)
		}
		truncated := map[string]bool{}
		for _, w := range m.res.Workflows {
			truncated[w.Name] = w.Truncated
		}
		for _, f := range engine.Findings(measured, truncated) {
			// "rules check" prints these itself, once, in its own output.
			if cmdName == "rules check" && isEntryFinding(f) {
				continue
			}
			fmt.Fprintf(stderr, "pathkit %s: warning: %s\n", cmdName, f)
		}
		var final []classify.Classification
		for _, w := range m.res.Workflows {
			for _, p := range w.Paths {
				final = append(final, p.Class)
			}
		}
		m.classNotes = append(engine.Notes(final, measured), unexplainedNotes(m.res)...)
		// A part of the map left out of the list as impossible is one claim,
		// and it rests on the same assumptions as a listed path would.
		for _, w := range m.res.Workflows {
			for _, pg := range w.Pruned {
				if !pg.Unpruned {
					final = append(final, pg.Class)
				}
			}
		}
		m.assumptions = engine.Assumptions(final)
		m.panel = engine.AssumptionPanel(final)
		m.customLabels = engine.CustomLabels()
	}
	return m, nil
}

// finish is what coverage and report do once their output is built:
// print it, write --out, then --clean (only now that the report was fully
// produced: exit 0 or 2, never after an error), and
// last the --fail-under check (exit 2).
//
// printed goes to stdout; plain (the same text without colour) goes to
// the --out file, which is always plain text.
//
// extra, when not nil, runs after --out and before --clean (report's
// --html): if it fails, the traces are kept.
func finish(cmd *cobra.Command, cmdName string, m *measured, printed, plain, outFile string, clean bool, extra func() error) error {
	fmt.Fprint(cmd.OutOrStdout(), printed)
	if outFile != "" {
		if err := os.WriteFile(outFile, []byte(plain), 0o644); err != nil {
			return userError("could not write --out file: %v", err)
		}
	}
	if extra != nil {
		if err := extra(); err != nil {
			return err
		}
	}
	if m.ci != nil && m.ci.write {
		file := baseline.From(m.res)
		file.RuleAnchors = m.ci.anchors
		if err := baseline.Write(m.ci.baselineAbs, file); err != nil {
			return userError("could not write the baseline file: %v", err)
		}
		paths := 0
		for _, w := range m.res.Workflows {
			paths += len(w.Paths)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "pathkit %s: wrote the baseline %s: %s in %s\n", cmdName, m.ci.baselinePath, count(paths, "path"), count(len(m.res.Workflows), "workflow"))
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
	// Every exit-2 reason is printed; the last one is the error itself, so
	// --fail-under alone prints exactly what it printed before.
	var fails [][]string
	if m.threshold != nil {
		c := m.res.Breakdown.For(m.basis)
		if v := coverage.Percent(c.Covered, c.Paths); v < *m.threshold {
			msg := fmt.Sprintf("coverage %s is below %s %s", render.Pct(v, m.threshold), m.thresholdFrom, render.Threshold(*m.threshold))
			if m.showClass {
				msg += " (basis: " + string(m.basis) + ")"
			}
			fails = append(fails, []string{msg})
		}
	}
	fails = append(fails, m.ci.failures(m.res)...)
	if len(fails) == 0 {
		return nil
	}
	for _, f := range fails[:len(fails)-1] {
		fmt.Fprintf(cmd.ErrOrStderr(), "pathkit %s: %s\n", cmdName, strings.Join(f, "\n"))
	}
	last := fails[len(fails)-1]
	return &exitError{code: ExitBelowThreshold, msg: last[0], detail: last[1:]}
}

// excludedBy names what decided the scope, for the excluded line: the
// config file, --include, --exclude. Empty when no scope is in use (or
// with --all).
func excludedBy(sc *scoped, sf scopeFlags) []string {
	if sf.all {
		return nil
	}
	var by []string
	if cfg := sc.cfg; cfg != nil && ((cfg.HasInclude && sf.include == nil) || len(cfg.Exclude) > 0) {
		by = append(by, filepath.Base(cfg.Path))
	}
	if sf.include != nil {
		by = append(by, "--include")
	}
	if len(sf.exclude) > 0 {
		by = append(by, "--exclude")
	}
	return by
}

// classificationEngine builds the classifier from the config's
// classification section, with workflow names resolved to full names and
// every //pathkit:untestable comment handed over. nil means classification
// is off (every path stays testable).
func classificationEngine(sc *scoped, cmdName string, stderr io.Writer) (*classify.Engine, error) {
	cfg := sc.cfg
	if cfg == nil {
		return nil, nil
	}
	cl := cfg.Classification
	if !cl.Enabled {
		for _, r := range sc.mapped {
			if len(r.graph.Marks) > 0 {
				fmt.Fprintf(stderr, "pathkit %s: warning: %s has //pathkit:untestable comments, but classification is not enabled, so they are ignored (set \"enabled\": true under \"classification\" in %s)\n", cmdName, r.wf.Name, cfg.Path)
				break
			}
		}
		if len(cl.Overrides)+len(cl.Rules)+len(cl.Custom)+len(cl.Explained) > 0 {
			fmt.Fprintf(stderr, "pathkit %s: warning: classification is not enabled, so the overrides, rules and custom labels in %s are ignored (set \"enabled\": true under \"classification\")\n", cmdName, cfg.Path)
		}
		if cl.CI.Baseline != "" || cl.CI.FailUnderTestable != nil {
			fmt.Fprintf(stderr, "pathkit %s: warning: classification is not enabled, so classification.ci in %s is not checked (set \"enabled\": true under \"classification\")\n", cmdName, cfg.Path)
		}
		return nil, nil
	}
	where := "the configured packages (" + strings.Join(cfg.Packages, ", ") + ")"
	resolve := func(what, name string) (string, error) {
		full, err := scope.MatchName(sc.names, name, what+" workflow", where)
		if err != nil {
			return "", userError("%s: %v", cfg.Path, err)
		}
		return full, nil
	}
	resolved := cl
	resolved.Overrides = nil
	for _, o := range cl.Overrides {
		w, err := resolve("classification.paths."+map[bool]string{true: "mark_untestable", false: "mark_testable"}[o.Untestable]+" ("+o.StableID+")", o.Workflow)
		if err != nil {
			return nil, err
		}
		o.Workflow = w
		resolved.Overrides = append(resolved.Overrides, o)
	}
	resolved.Rules = nil
	for _, r := range cl.Rules {
		w, err := resolve("classification.rules ("+r.ID+")", r.Workflow)
		if err != nil {
			return nil, err
		}
		r.Workflow = w
		resolved.Rules = append(resolved.Rules, r)
	}
	engine, err := classify.NewEngine(resolved)
	if err != nil {
		return nil, userError("%s: classification: %v", cfg.Path, err)
	}
	var explained []scope.ExplainedTrace
	for _, x := range cl.Explained {
		w, err := resolve("classification.explained_traces ("+x.StepsID+")", x.Workflow)
		if err != nil {
			return nil, err
		}
		x.Workflow = w
		explained = append(explained, x)
	}
	engine.SetExplained(explained)
	// The module(s) of the workflows are searched for a custom data converter
	// (the assumption default-json-converter), unless the engine or the
	// assumption is off anyway.
	if !cl.EngineOff && cl.JSONConverter != "off" {
		var hits []string
		seenRoot := map[string]bool{}
		for _, r := range sc.mapped {
			root := load.ModuleRoot(r.wf.Filename)
			if root == "" || seenRoot[root] {
				continue
			}
			seenRoot[root] = true
			hits = append(hits, load.CustomConverterUse(root)...)
		}
		engine.SetConverterScan(hits)
	}
	// The same for a custom interceptor (the assumption no-custom-interceptor).
	if !cl.EngineOff && cl.NoCustomInterceptor != "off" {
		var hits []string
		seenRoot := map[string]bool{}
		for _, r := range sc.mapped {
			root := load.ModuleRoot(r.wf.Filename)
			if root == "" || seenRoot[root] {
				continue
			}
			seenRoot[root] = true
			hits = append(hits, load.CustomInterceptorUse(root)...)
		}
		engine.SetInterceptorScan(hits)
	}
	for _, r := range sc.mapped {
		engine.AddMarks(r.wf.Name, r.graph.Marks)
		if err := engine.AddGraph(r.wf.Name, r.graph); err != nil {
			return nil, userError("%s: %v", cfg.Path, err)
		}
	}
	return engine, nil
}

// unexplainedNotes are the lines of the classification block about traces
// that fit no path: one per group, saying whether it was accepted.
func unexplainedNotes(res coverage.Result) []string {
	var out []string
	for _, w := range res.Workflows {
		downgraded := 0
		for _, p := range w.Paths {
			if p.Class.Origin == classify.OriginUnexplained {
				downgraded++
			}
		}
		for _, u := range w.Unexplained {
			files := count(u.Count, "trace file")
			if u.Acknowledged {
				out = append(out, fmt.Sprintf("%s: %s fits no path (steps id %s), acknowledged: %s", w.Name, files, u.StepsID, u.Reason))
				continue
			}
			out = append(out, fmt.Sprintf("%s: %s fits no path (steps id %s); not acknowledged, so its cannot-be-tested claims by detectors and rules are shown as needs-review (%s)", w.Name, files, u.StepsID, count(downgraded, "path")))
		}
	}
	return out
}

// isEntryFinding says whether a finding of the engine is about a rule or an
// override that hits no path (stale, or beyond the path cap).
func isEntryFinding(f string) bool {
	return strings.Contains(f, "stale rule") || strings.Contains(f, "stale entry") || strings.Contains(f, "matches none of the listed paths")
}
