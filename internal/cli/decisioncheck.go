package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
)

// decisionCheckFile is where the result is kept, for the reports.
const decisionCheckFile = ".pathkit/decision-check.json"

type checkOptions struct {
	measure      measureOptions
	sample       int
	base         string
	workflow     string
	parallel     int
	timeout      time.Duration
	json         bool
	allowNetwork bool
	dryRun       bool
}

func newCheckDecisionsCommand() *cobra.Command {
	var opt checkOptions
	cmd := &cobra.Command{
		Use:   "check-decisions [folder|folder/...]",
		Short: "Check that your tests fail when a decision goes the other way",
		Long: `Takes the decisions your tests already cover and forces each one to take
another outcome that can happen (an if goes the other way; an activity call
that succeeded fails with an error the code can handle), then re-runs only
the tests that went through that decision. A decision is:

  noticed            a test failed
  survived           every test still passed: no test checks that decision
  no visible effect  it survived, and the model shows the change cannot alter
                     anything the workflow does outside itself
  flaky              the result changed when it was run again

The share of decisions the tests noticed is "Tests notice wrong decisions" in
the reports. The result is kept in .pathkit/decision-check.json.

Run "pathkit test" first (it records which tests went through which decision).
The tests are run with no network, like "pathkit test --no-network": the
command refuses when that is not available, unless --allow-network is given.
Only decisions that can be forced are checked today: a plain if, and the error
check of an activity, child workflow or timer call; switches, loops and waits
on several things are listed as not checked.

Your source files are never changed: the forcing happens in the marked-up copy
used by go test -overlay, and only when PATHKIT_FLIP names the outcome.`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runCheckDecisions(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.IntVar(&opt.sample, "sample", 0, "check only `N` decisions, picked the same way every time (0: all)")
	f.StringVar(&opt.base, "base", "", "check only the decisions that are new or changed compared with this git `ref`")
	f.StringVar(&opt.workflow, "workflow", "", "check only this `workflow`")
	f.IntVarP(&opt.parallel, "parallel", "p", 1, "how many forced runs at the same time")
	f.DurationVar(&opt.timeout, "timeout", 10*time.Minute, "the longest one forced run may take, at most; a run is also cut at 5 times the normal run (a run that takes longer counts as noticed)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.allowNetwork, "allow-network", false, "run the tests with the network on (the default refuses to, unless the network can be switched off)")
	f.BoolVar(&opt.dryRun, "list", false, "only list the decisions that would be checked")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

// mutant is one decision to force.
type mutant struct {
	wf       coverage.WorkflowResult
	o        coverage.OutcomeResult
	tests    []string
	allTests bool // the tests that went through it are not all known: run the package
	pkg      string
	// testPkgs are the packages that hold those tests: they are often not the
	// package of the workflow (a tests/ folder next to it).
	testPkgs []string
}

func runCheckDecisions(cmd *cobra.Command, target string, opt checkOptions) error {
	runner := runnerOptions{noNetwork: !opt.allowNetwork}
	goBin, goPrefix, err := runner.command()
	if err != nil {
		if !opt.allowNetwork {
			return userError("%v (or pass --allow-network to run the tests with the network on)", err)
		}
		return err
	}
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	opt.measure.function = opt.workflow
	opt.measure.noCI = true
	m, err := measure(cmd, "check-decisions", target, opt.measure)
	if err != nil {
		return err
	}
	var changedOnly map[string]bool
	if opt.base != "" {
		changedOnly, err = newOutcomeIDs(cmd, target, opt)
		if err != nil {
			return err
		}
	}
	dir, _, _, err := load.Resolve(m.sc.target)
	if err != nil {
		return userError("%s", err)
	}
	muts, skipped := pickMutants(m.res, changedOnly, opt.sample, dir)
	if opt.dryRun {
		var b strings.Builder
		for _, mu := range muts {
			fmt.Fprintf(&b, "%s:%d  %s forced to %q  (tests: %s)\n", filepath.ToSlash(load.DisplayPath(mu.o.File)), mu.o.Line, advice.DecisionText(mu.o), advice.OutcomeLabel(mu.o), testsText(mu))
		}
		fmt.Fprintf(&b, "%s would be checked; %d not checked.\n", count(len(muts), "forced decision"), len(skipped))
		fmt.Fprint(cmd.OutOrStdout(), b.String())
		return nil
	}
	if len(muts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No covered decision can be forced: nothing to check. Run \"pathkit test\" first so that PathKit knows which decisions your tests cover.")
		return nil
	}
	if len(m.sc.mapped) == 0 {
		return userError("no workflow in scope to force")
	}
	tmpTraces, err := os.MkdirTemp("", "pathkit-flip-traces-")
	if err != nil {
		return userError("could not make a temporary folder: %v", err)
	}
	defer os.RemoveAll(tmpTraces)
	overlay, err := writeOverlay(m.sc.mapped, tmpTraces)
	if err != nil {
		return err
	}
	dc := coverage.DecisionCheck{Date: time.Now().UTC().Format("2006-01-02")}
	dc.Skipped = skipped
	// the tests of a package must pass before anything is changed
	broken := map[string]string{}
	var mu sync.Mutex
	goTest := func(env []string, ctx context.Context, args ...string) (string, error) {
		full := append(append([]string(nil), goPrefix...), append([]string{"test", "-overlay=" + overlay, "-count=1"}, args...)...)
		c := exec.CommandContext(ctx, goBin, full...)
		stopWithChildren(c) // a forced run can loop for ever: stop the test program too
		c.Dir = dir
		c.Env = append(os.Environ(), "PATHKIT_TRACES=off", "PATHKIT_TESTNAMES=off")
		c.Env = append(c.Env, env...)
		if runtime.GOOS != "windows" {
			if abs, err := filepath.Abs(dir); err == nil {
				if real, err := filepath.EvalSymlinks(abs); err == nil {
					c.Env = append(c.Env, "PWD="+real)
				}
			}
		}
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		return out.String(), err
	}
	pkgs := map[string]bool{}
	for _, mt := range muts {
		for _, p := range mt.testPkgs {
			pkgs[p] = true
		}
	}
	var pkgList []string
	for p := range pkgs {
		pkgList = append(pkgList, p)
	}
	sort.Strings(pkgList)
	baseline := map[string]time.Duration{}
	for _, p := range pkgList {
		ctx, cancel := context.WithTimeout(context.Background(), opt.timeout)
		began := time.Now()
		out, err := goTest(nil, ctx, p)
		baseline[p] = time.Since(began)
		cancel()
		if err != nil {
			broken[p] = firstFailure(out)
			dc.Skipped = append(dc.Skipped, fmt.Sprintf("%s: its tests fail before any change (%s), so no decision of it was forced", p, broken[p]))
		}
	}
	jobs := make(chan int)
	results := make([]coverage.DecisionMutant, len(muts))
	workers := max(1, opt.parallel)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				mt := muts[i]
				res := coverage.DecisionMutant{Branch: mt.o.ID, Workflow: mt.wf.Name, File: filepath.ToSlash(load.DisplayPath(mt.o.File)), Line: mt.o.Line, Decision: advice.DecisionText(mt.o), Outcome: advice.OutcomeLabel(mt.o), Tests: mt.tests}
				bad := false
				for _, p := range mt.testPkgs {
					if _, b := broken[p]; b {
						bad = true
					}
				}
				if bad {
					res.Result, res.Detail = "error", "the package's tests fail before any change"
				} else {
					// a forced run that loops for ever is cut short: 5 times the
					// normal run of its packages, at least a minute, at most --timeout
					limit := time.Minute
					for _, p := range mt.testPkgs {
						limit = max(limit, 5*baseline[p])
					}
					res.Result, res.Detail = forceOnce(goTest, mt, min(limit, opt.timeout))
				}
				mu.Lock()
				results[i] = res
				mu.Unlock()
			}
		}()
	}
	for i := range muts {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	byID := map[string]mutant{}
	for _, mt := range muts {
		byID[mt.o.ID] = mt
	}
	for _, r := range results {
		if r.Result == "survived" && noVisibleEffect(byID[r.Branch]) {
			r.Result = "no visible effect"
		}
		dc.Mutants = append(dc.Mutants, r)
		switch r.Result {
		case "noticed":
			dc.Noticed++
		case "survived":
			dc.Survived++
			mt := byID[r.Branch]
			dc.Survivors = append(dc.Survivors, coverage.DecisionSurvivor{Branch: r.Branch, Workflow: r.Workflow, File: r.File, Line: r.Line, Decision: r.Decision, Outcome: r.Outcome, Sentence: survivorSentence(mt.o)})
		case "no visible effect":
			dc.NoEffect++
		case "flaky":
			dc.Flaky++
		}
		if r.Result != "error" {
			dc.Checked++
		}
	}
	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(decisionCheckFile), 0o755); err == nil {
		if err := os.WriteFile(decisionCheckFile, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "pathkit check-decisions: could not keep the result in %s: %v\n", decisionCheckFile, err)
		}
	}
	if opt.json {
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), decisionCheckText(dc))
	return nil
}

// forceOnce runs the tests that went through a decision with it forced, once,
// and once more when nothing failed (a decision that no test noticed is only
// reported as such after a second look: a test that fails by itself is flaky).
func forceOnce(goTest func([]string, context.Context, ...string) (string, error), mt mutant, timeout time.Duration) (string, string) {
	args := []string{}
	if !mt.allTests && len(mt.tests) > 0 {
		quoted := make([]string, len(mt.tests))
		for i, t := range mt.tests {
			quoted[i] = regexp.QuoteMeta(t)
		}
		args = append(args, "-run", "^("+strings.Join(quoted, "|")+")$")
	}
	args = append(args, mt.testPkgs...)
	try := func() (string, string) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := goTest([]string{"PATHKIT_FLIP=" + mt.o.ID}, ctx, args...)
		switch {
		case err == nil:
			if !someTestRan(out) {
				return "error", "no test ran (the tests that went through it were not found in the packages tried)"
			}
			return "survived", ""
		case ctx.Err() != nil:
			return "noticed", "the run took longer than " + timeout.String()
		case strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]"):
			return "error", "the marked-up copy did not build: " + firstFailure(out)
		}
		return "noticed", firstFailure(out)
	}
	res, detail := try()
	if res != "survived" {
		return res, detail
	}
	// a second look: it must still survive, or the first run was not a fair one
	if again, d := try(); again != "survived" {
		return "flaky", d
	}
	return "survived", ""
}

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: .*$`)

func firstFailure(out string) string {
	if m := failLine.FindString(out); m != "" {
		return strings.TrimSpace(m)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		if len(last) > 160 {
			last = last[:160]
		}
		return last
	}
	return ""
}

func testsText(mt mutant) string {
	if mt.allTests || len(mt.tests) == 0 {
		return "all tests of the package"
	}
	return strings.Join(mt.tests, ", ")
}

// pickMutants lists the decisions to force: every outcome that can happen of a
// decision some test covered, when the decision can be forced. changed, when
// not nil, keeps only the outcomes that are new or changed (by branch ID).
// sample keeps N of them, chosen by a hash of the branch ID: the same ones on
// every run of the same code.
func pickMutants(res coverage.Result, changed map[string]bool, sample int, testRoot string) ([]mutant, []string) {
	type dec struct {
		file, text, fn string
		line           int
	}
	var out []mutant
	var skipped []string
	seen := map[string]bool{}
	notForced := map[string]bool{}
	for _, w := range res.Workflows {
		groups := map[dec][]coverage.OutcomeResult{}
		var order []dec
		for _, o := range w.Outcomes {
			k := dec{o.File, o.Decision, o.Func, o.Line}
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], o)
		}
		for _, k := range order {
			outs := groups[k]
			coveredN := 0
			for _, o := range outs {
				if o.Status == coverage.OutcomeCovered {
					coveredN++
				}
			}
			if coveredN == 0 {
				continue
			}
			for _, o := range outs {
				if o.Status == coverage.OutcomeImpossible || seen[o.ID] {
					continue
				}
				if o.Flip != "" {
					key := w.Name + "|" + o.Decision + "|" + o.Flip
					if !notForced[key] {
						notForced[key] = true
						skipped = append(skipped, fmt.Sprintf("%s:%d %s: not checked (%s)", filepath.ToSlash(load.DisplayPath(o.File)), o.Line, advice.DecisionText(o), o.Flip))
					}
					continue
				}
				if changed != nil && !changed[w.Name+"\x00"+o.ID] {
					continue
				}
				// forcing this outcome changes only the runs that took another one:
				// those are the tests to run; with none, forcing it changes nothing
				union := map[string]bool{}
				unnamed, others := false, 0
				for _, o2 := range outs {
					if o2.ID == o.ID || o2.Status != coverage.OutcomeCovered {
						continue
					}
					others++
					for _, t := range o2.Tests {
						union[t] = true
					}
					unnamed = unnamed || o2.Unnamed
				}
				if others == 0 {
					continue
				}
				var tests []string
				for t := range union {
					tests = append(tests, t)
				}
				sort.Strings(tests)
				seen[o.ID] = true
				out = append(out, mutant{wf: w, o: o, tests: tests, allTests: unnamed || len(tests) == 0, pkg: packageOf(w), testPkgs: testPackages(testRoot, tests, unnamed || len(tests) == 0, packageOf(w))})
			}
		}
	}
	if sample > 0 && len(out) > sample {
		sort.SliceStable(out, func(i, j int) bool { return hashOf(out[i].o.ID) < hashOf(out[j].o.ID) })
		out = out[:sample]
	}
	return out, skipped
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte("pathkit-sample\n" + s))
	return hex.EncodeToString(sum[:])
}

func packageOf(w coverage.WorkflowResult) string {
	if w.Package != "" {
		return w.Package
	}
	return "./..."
}

// noVisibleEffect: forcing the outcome cannot change anything the workflow does
// outside itself, as far as the scenario model can tell: the scenarios of the
// listed paths through it are exactly those of the paths through the other
// outcomes of the same decision.
func noVisibleEffect(mt mutant) bool {
	w := mt.wf
	if !w.Scenarios.Measured {
		return false
	}
	mine := map[string]bool{}
	other := map[string]bool{}
	mineIDs := map[string]bool{}
	for _, e := range mt.o.Exits {
		mineIDs[e] = true
	}
	var siblings []coverage.OutcomeResult
	for _, o := range w.Outcomes {
		if o.File == mt.o.File && o.Line == mt.o.Line && o.Decision == mt.o.Decision && o.Func == mt.o.Func && o.ID != mt.o.ID {
			siblings = append(siblings, o)
		}
	}
	sibIDs := map[string]bool{}
	for _, s := range siblings {
		for _, e := range s.Exits {
			sibIDs[e] = true
		}
	}
	for _, p := range w.Paths {
		hitMine, hitSib := false, false
		for _, s := range p.Path.Steps {
			id := s.Exit.ID.String()
			hitMine = hitMine || mineIDs[id]
			hitSib = hitSib || sibIDs[id]
		}
		if hitMine {
			mine[p.ScenarioID] = true
		}
		if hitSib {
			other[p.ScenarioID] = true
		}
	}
	if len(mine) == 0 || len(mine) != len(other) {
		return false
	}
	for k := range mine {
		if !other[k] {
			return false
		}
	}
	return true
}

func survivorSentence(o coverage.OutcomeResult) string {
	return fmt.Sprintf("%s is forced to \"%s\" every time, and no test fails", advice.DecisionText(o), advice.OutcomeLabel(o))
}

// readDecisionCheck reads the last decision check, or nil when there is none
// (or the file is not one: the reports then say nothing about it).
func readDecisionCheck() *coverage.DecisionCheck {
	data, err := os.ReadFile(decisionCheckFile)
	if err != nil {
		return nil
	}
	var dc coverage.DecisionCheck
	if json.Unmarshal(data, &dc) != nil || dc.Date == "" {
		return nil
	}
	return &dc
}

func decisionCheckText(dc coverage.DecisionCheck) string {
	var b strings.Builder
	n := dc.Noticed + dc.Survived
	fmt.Fprintf(&b, "Decisions checked: %d · noticed by a test: %d · no test fails: %d · no visible effect: %d · flaky: %d\n", dc.Checked, dc.Noticed, dc.Survived, dc.NoEffect, dc.Flaky)
	if n > 0 {
		fmt.Fprintf(&b, "Tests notice wrong decisions: %.1f%% (%d of %d)\n", dc.Score(), dc.Noticed, n)
	} else {
		b.WriteString("Tests notice wrong decisions: nothing to score\n")
	}
	if len(dc.Survivors) > 0 {
		b.WriteString("\nDecisions no test noticed\n")
		for _, s := range dc.Survivors {
			fmt.Fprintf(&b, "  %s:%d — %s\n", s.File, s.Line, s.Sentence)
		}
	}
	if len(dc.Skipped) > 0 {
		b.WriteString("\nNot checked\n")
		for _, s := range dc.Skipped {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	}
	fmt.Fprintf(&b, "\nSaved in %s. Next: pathkit report   (shows the score)\n", decisionCheckFile)
	return b.String()
}

// newOutcomeIDs is the set of outcomes that are new or changed compared with a git
// ref, by workflow and branch ID (the same comparison "pathkit diff" makes).
func newOutcomeIDs(cmd *cobra.Command, target string, opt checkOptions) (map[string]bool, error) {
	dopt := diffOptions{measure: opt.measure, base: opt.base}
	dopt.measure.tracesOptional = true
	dopt.measure.noCI = true
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, userError("--base needs a git repository: %v", err)
	}
	root = strings.TrimSpace(root)
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	realCwd := cwd
	if r, err := filepath.EvalSymlinks(cwd); err == nil {
		realCwd = r
	}
	rel, err := filepath.Rel(root, realCwd)
	if err != nil {
		return nil, userError("could not place %s in the repository: %v", cwd, err)
	}
	sha, err := gitOutput(root, "rev-parse", "--verify", "--quiet", opt.base+"^{commit}")
	if err != nil {
		return nil, userError("%q is not a git ref (branch, tag or commit) of this repository", opt.base)
	}
	sha = strings.TrimSpace(sha)
	now, err := measure(cmd, "check-decisions", target, dopt.measure)
	if err != nil {
		return nil, err
	}
	nowSnap := takeSnapshot(now)
	var sink bytes.Buffer
	baseSnap, err := baseSnapshot(root, rel, sha, target, dopt, &sink)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k := range nowSnap.outcomes {
		if _, had := baseSnap.outcomes[k]; !had {
			out[k] = true
		}
	}
	return out, nil
}

// someTestRan: go test prints "ok  pkg" for a package that ran something and
// "ok  pkg [no tests to run]" for one that matched nothing; a forced run in
// which nothing ran says nothing about the decision.
func someTestRan(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "ok") && !strings.Contains(l, "no tests to run") {
			return true
		}
	}
	return false
}

var testFuncRe = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)

// testPackages names the packages (as ./folder) that define the given tests.
// When the tests are not all known, or one is not found, it is "./...".
func testPackages(root string, tests []string, all bool, fallback string) []string {
	if all || len(tests) == 0 {
		return []string{fallback}
	}
	want := map[string]bool{}
	for _, t := range tests {
		want[strings.SplitN(t, "/", 2)[0]] = true
	}
	found := map[string]bool{}
	pkgs := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".pathkit", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range testFuncRe.FindAllStringSubmatch(string(data), -1) {
			if want[m[1]] {
				found[m[1]] = true
				rel, err := filepath.Rel(root, filepath.Dir(path))
				if err != nil {
					rel = "."
				}
				pkgs["./"+filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	if len(found) < len(want) {
		return []string{"./..."}
	}
	var out []string
	for p := range pkgs {
		if p == "./." {
			p = "."
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
