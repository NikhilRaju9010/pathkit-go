package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

type diffOptions struct {
	measure           measureOptions
	base              string
	markdown, asJSON  bool
	failOnUntestedNew bool
}

func newDiffCommand() *cobra.Command {
	var opt diffOptions
	cmd := &cobra.Command{
		Use:   "diff --base <git-ref> [folder|folder/...]",
		Short: "Show what a change adds to or takes from the workflows, and whether tests cover the new parts",
		Long: `Compares the workflows of this folder with the same folder at a git ref (a
branch, a tag or a commit, for example the branch a pull request goes into).
PathKit checks the ref out in a temporary git worktree (removed afterwards,
also when something goes wrong), builds the same maps there, and lists by id
what is new, removed or changed: workflows, decisions, scenarios and handlers.
For the new decisions and scenarios it says whether the traces of this run
cover them.

--markdown prints a text for a pull request comment, --json the same as data.
With --fail-on-untested-new the command exits with code 2 when a new decision
(no exit of it taken by any test) or a new scenario has no test, so a pull
request cannot add untested behaviour.`,
		Example: "  pathkit diff --base origin/main\n  pathkit diff --base main --markdown --fail-on-untested-new",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runDiff(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.base, "base", "", "the git `ref` to compare with (required)")
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.BoolVar(&opt.markdown, "markdown", false, "print markdown, for a pull request comment")
	f.BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.failOnUntestedNew, "fail-on-untested-new", false, "exit with code 2 when a new decision or scenario has no test")
	addScopeFlags(cmd, &opt.measure.scope, false)
	return cmd
}

// ---- snapshots ----

type decSnap struct {
	Workflow string   `json:"workflow"`
	Label    string   `json:"decision"`
	Where    string   `json:"where"`
	Exits    []string `json:"exits"`
	Taken    int      `json:"exitsTaken"`
}

type scSnap struct {
	Workflow string   `json:"workflow"`
	ID       string   `json:"id"`
	Steps    []string `json:"steps"`
	Where    string   `json:"where"`
	Covered  bool     `json:"covered"`
}

type hdSnap struct {
	Owner, Kind, Name string
	Where             string
	Branches          int
	Exercised         string
}

type snapshot struct {
	workflows map[string]bool
	decisions map[string]decSnap
	scenarios map[string]scSnap
	handlers  map[string]hdSnap
	// unmeasured are the workflows whose scenarios could not be counted.
	unmeasured map[string]string
}

func takeSnapshot(m *measured) snapshot {
	s := snapshot{workflows: map[string]bool{}, decisions: map[string]decSnap{}, scenarios: map[string]scSnap{}, handlers: map[string]hdSnap{}, unmeasured: map[string]string{}}
	graphs := map[string]*model.Graph{}
	for _, w := range m.workflows {
		graphs[w.Name] = w.Graph
	}
	for _, w := range m.res.Workflows {
		s.workflows[w.Name] = true
		took := w.TookExits()
		if g := graphs[w.Name]; g != nil {
			for _, j := range g.Junctions {
				d := decSnap{Workflow: w.Name, Label: j.Label, Where: fmt.Sprintf("%s:%d", filepath.ToSlash(load.DisplayPath(j.Pos.Filename)), j.Pos.Line)}
				for _, e := range j.Exits {
					d.Exits = append(d.Exits, e.Label)
					if took[e.ID.String()] {
						d.Taken++
					}
				}
				s.decisions[w.Name+"\x00"+j.StableLabel()] = d
			}
		}
		if !w.Scenarios.Measured {
			s.unmeasured[w.Name] = w.Scenarios.Why
			continue
		}
		byNumber := map[int]coverage.PathResult{}
		for _, p := range w.Paths {
			byNumber[p.Number] = p
		}
		for _, sc := range w.Scenarios.Items {
			where := ""
			if p, ok := byNumber[sc.Paths[0]]; ok && len(p.Path.Steps) > 0 {
				pos := p.Path.Steps[0].Junction.Pos
				where = fmt.Sprintf("%s:%d", filepath.ToSlash(load.DisplayPath(pos.Filename)), pos.Line)
			}
			s.scenarios[w.Name+"\x00"+sc.ID] = scSnap{Workflow: w.Name, ID: sc.ID, Steps: sc.Steps, Where: where, Covered: sc.Covered}
		}
	}
	for _, h := range m.res.Handlers {
		s.handlers[h.Owner+"\x00"+h.Kind+"\x00"+h.Name] = hdSnap{Owner: h.Owner, Kind: h.Kind, Name: h.Name, Where: h.Where, Branches: h.Branches, Exercised: h.Exercised}
	}
	return s
}

// ---- the comparison ----

type change struct {
	Kind   string `json:"kind"`   // "workflow", "decision", "scenario", "handler"
	Change string `json:"change"` // "new", "removed", "changed"
	Name   string `json:"name"`
	Where  string `json:"where,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Tested is set for new decisions and scenarios: whether the traces of this
	// run cover them.
	Tested *bool `json:"tested,omitempty"`
}

type diffResult struct {
	Base      string   `json:"base"`
	Changes   []change `json:"changes"`
	Untested  int      `json:"untestedNew"`
	Notes     []string `json:"notes,omitempty"`
	NoChanges bool     `json:"noChanges"`
}

func compare(base, now snapshot) diffResult {
	var d diffResult
	ptr := func(b bool) *bool { return &b }
	add := func(c change) { d.Changes = append(d.Changes, c) }

	for _, name := range sortedKeys(now.workflows) {
		if !base.workflows[name] {
			add(change{Kind: "workflow", Change: "new", Name: name})
		}
	}
	for _, name := range sortedKeys(base.workflows) {
		if !now.workflows[name] {
			add(change{Kind: "workflow", Change: "removed", Name: name})
		}
	}
	for _, k := range sortedKeys(now.decisions) {
		n := now.decisions[k]
		o, had := base.decisions[k]
		switch {
		case !had:
			tested := n.Taken > 0
			if !tested {
				d.Untested++
			}
			add(change{Kind: "decision", Change: "new", Name: n.Workflow + ": " + n.Label, Where: n.Where, Tested: ptr(tested),
				Detail: fmt.Sprintf("%d of %d exits taken by a test", n.Taken, len(n.Exits))})
		case strings.Join(o.Exits, ",") != strings.Join(n.Exits, ","):
			add(change{Kind: "decision", Change: "changed", Name: n.Workflow + ": " + n.Label, Where: n.Where,
				Detail: fmt.Sprintf("exits were %s, now %s", strings.Join(o.Exits, "/"), strings.Join(n.Exits, "/"))})
		}
	}
	for _, k := range sortedKeys(base.decisions) {
		if _, still := now.decisions[k]; !still {
			o := base.decisions[k]
			add(change{Kind: "decision", Change: "removed", Name: o.Workflow + ": " + o.Label, Where: o.Where})
		}
	}
	for _, k := range sortedKeys(now.scenarios) {
		n := now.scenarios[k]
		if _, had := base.scenarios[k]; !had {
			if !n.Covered {
				d.Untested++
			}
			add(change{Kind: "scenario", Change: "new", Name: n.Workflow + " " + n.ID, Where: n.Where, Tested: ptr(n.Covered), Detail: strings.Join(n.Steps, " → ")})
		}
	}
	for _, k := range sortedKeys(base.scenarios) {
		if _, still := now.scenarios[k]; !still {
			o := base.scenarios[k]
			add(change{Kind: "scenario", Change: "removed", Name: o.Workflow + " " + o.ID, Where: o.Where, Detail: strings.Join(o.Steps, " → ")})
		}
	}
	for _, k := range sortedKeys(now.handlers) {
		n := now.handlers[k]
		o, had := base.handlers[k]
		label := fmt.Sprintf("%s %s %s", n.Owner, n.Kind, n.Name)
		switch {
		case !had:
			add(change{Kind: "handler", Change: "new", Name: label, Where: n.Where, Detail: "exercised by a test: " + n.Exercised})
		case o.Branches != n.Branches:
			add(change{Kind: "handler", Change: "changed", Name: label, Where: n.Where, Detail: fmt.Sprintf("branches were %d, now %d", o.Branches, n.Branches)})
		}
	}
	for _, k := range sortedKeys(base.handlers) {
		if _, still := now.handlers[k]; !still {
			o := base.handlers[k]
			add(change{Kind: "handler", Change: "removed", Name: fmt.Sprintf("%s %s %s", o.Owner, o.Kind, o.Name), Where: o.Where})
		}
	}
	for _, w := range sortedKeys(now.unmeasured) {
		d.Notes = append(d.Notes, fmt.Sprintf("%s: scenarios are not measured (%s), so only its decisions are compared", w, now.unmeasured[w]))
	}
	d.NoChanges = len(d.Changes) == 0
	return d
}

// ---- running ----

func runDiff(cmd *cobra.Command, target string, opt diffOptions) error {
	if strings.TrimSpace(opt.base) == "" {
		return userError("--base needs a git ref, for example --base origin/main")
	}
	if opt.markdown && opt.asJSON {
		return userError("--markdown and --json cannot be used together")
	}
	opt.measure.tracesOptional = true
	opt.measure.noCI = true
	opt.measure.scope.everyPackage = true
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return userError("pathkit diff needs a git repository: %v", err)
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
		return userError("could not place %s in the repository: %v", cwd, err)
	}
	sha, err := gitOutput(root, "rev-parse", "--verify", "--quiet", opt.base+"^{commit}")
	if err != nil {
		return userError("%q is not a git ref (branch, tag or commit) of this repository", opt.base)
	}
	sha = strings.TrimSpace(sha)

	// the current state
	var sink bytes.Buffer
	now, err := measure(cmd, "diff", target, opt.measure)
	if err != nil {
		return err
	}
	nowSnap := takeSnapshot(now)

	// the base: a temporary worktree, removed afterwards whatever happens
	baseSnap, err := baseSnapshot(root, rel, sha, target, opt, &sink)
	if err != nil {
		return err
	}
	res := compare(baseSnap, nowSnap)
	res.Base = fmt.Sprintf("%s (%s)", opt.base, sha[:min(10, len(sha))])

	var out string
	switch {
	case opt.asJSON:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if res.Changes == nil {
			res.Changes = []change{}
		}
		if err := enc.Encode(res); err != nil {
			return err
		}
		out = buf.String()
	case opt.markdown:
		out = diffMarkdown(res)
	default:
		out = diffText(res)
	}
	fmt.Fprint(cmd.OutOrStdout(), out)
	if opt.failOnUntestedNew && res.Untested > 0 {
		return belowThresholdError("%s new %s no test (the traces of this run cover nothing of them)", count(res.Untested, "decision or scenario"), map[bool]string{true: "has", false: "have"}[res.Untested == 1])
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// baseSnapshot checks sha out in a temporary worktree, measures the same
// folder there and removes the worktree.
func baseSnapshot(root, rel, sha, target string, opt diffOptions, sink *bytes.Buffer) (snap snapshot, err error) {
	tmp, err := os.MkdirTemp("", "pathkit-diff-")
	if err != nil {
		return snap, userError("could not make a temporary folder: %v", err)
	}
	wt := filepath.Join(tmp, "base")
	defer func() {
		// git forgets the worktree, then the folder goes; both whatever happened
		rm := exec.Command("git", "worktree", "remove", "--force", wt)
		rm.Dir = root
		_ = rm.Run()
		prune := exec.Command("git", "worktree", "prune")
		prune.Dir = root
		_ = prune.Run()
		_ = os.RemoveAll(tmp)
	}()
	add := exec.Command("git", "worktree", "add", "--detach", "--quiet", wt, sha)
	add.Dir = root
	if out, err := add.CombinedOutput(); err != nil {
		return snap, userError("could not check %s out in a temporary worktree: %v %s", sha, err, strings.TrimSpace(string(out)))
	}
	back, err := os.Getwd()
	if err != nil {
		return snap, err
	}
	defer func() { _ = os.Chdir(back) }()
	dir := filepath.Join(wt, rel)
	if _, statErr := os.Stat(dir); statErr != nil {
		// the folder did not exist at the base: everything is new
		return snapshot{workflows: map[string]bool{}, decisions: map[string]decSnap{}, scenarios: map[string]scSnap{}, handlers: map[string]hdSnap{}, unmeasured: map[string]string{}}, nil
	}
	if err := os.Chdir(dir); err != nil {
		return snap, userError("could not enter %s: %v", dir, err)
	}
	quiet := &cobra.Command{}
	quiet.SetErr(sink)
	quiet.SetOut(sink)
	baseTarget := target
	if target != "" && !filepath.IsAbs(target) {
		// the same folder, below the worktree
		baseTarget = target
	}
	b, err := measure(quiet, "diff", baseTarget, opt.measure)
	if err != nil {
		return snap, userError("could not read the workflows at %s: %v", sha[:min(10, len(sha))], err)
	}
	return takeSnapshot(b), nil
}

// ---- output ----

func tally(res diffResult) map[string]map[string]int {
	t := map[string]map[string]int{}
	for _, c := range res.Changes {
		if t[c.Kind] == nil {
			t[c.Kind] = map[string]int{}
		}
		t[c.Kind][c.Change]++
	}
	return t
}

var kindOrder = []struct{ kind, title string }{{"workflow", "Workflows"}, {"decision", "Decisions"}, {"scenario", "Scenarios"}, {"handler", "Handlers"}}

func diffText(res diffResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Compared with %s\n", res.Base)
	if res.NoChanges {
		b.WriteString("No workflow, decision, scenario or handler is new, removed or changed.\n")
	}
	t := tally(res)
	for _, k := range kindOrder {
		if len(t[k.kind]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s: %d new, %d removed, %d changed\n", k.title, t[k.kind]["new"], t[k.kind]["removed"], t[k.kind]["changed"])
		for _, c := range res.Changes {
			if c.Kind != k.kind {
				continue
			}
			mark := map[string]string{"new": "+", "removed": "-", "changed": "~"}[c.Change]
			line := fmt.Sprintf("  %s %s", mark, c.Name)
			if c.Where != "" {
				line += " (" + c.Where + ")"
			}
			if c.Tested != nil {
				line += map[bool]string{true: " — tested", false: " — NO TEST"}[*c.Tested]
			}
			b.WriteString(line + "\n")
			if c.Detail != "" {
				b.WriteString("      " + c.Detail + "\n")
			}
		}
	}
	for _, n := range res.Notes {
		b.WriteString("\nNote: " + n + "\n")
	}
	if res.Untested > 0 {
		fmt.Fprintf(&b, "\n%s new %s no test yet.\n", count(res.Untested, "decision or scenario"), map[bool]string{true: "has", false: "have"}[res.Untested == 1])
	} else if !res.NoChanges {
		b.WriteString("\nEvery new decision and scenario is covered by a test.\n")
	}
	return b.String()
}

func diffMarkdown(res diffResult) string {
	var b strings.Builder
	b.WriteString("### PathKit: what this change adds\n\n")
	fmt.Fprintf(&b, "Compared with `%s`.\n\n", res.Base)
	if res.NoChanges {
		b.WriteString("No workflow, decision, scenario or handler is new, removed or changed.\n")
		return b.String()
	}
	for _, k := range kindOrder {
		var rows []change
		for _, c := range res.Changes {
			if c.Kind == k.kind {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s**\n\n| Change | What | Where | Tested |\n|---|---|---|---|\n", k.title)
		for _, c := range rows {
			tested := ""
			if c.Tested != nil {
				tested = map[bool]string{true: "yes", false: "**no**"}[*c.Tested]
			}
			what := strings.ReplaceAll(c.Name, "|", "\\|")
			if c.Detail != "" {
				what += "<br>" + strings.ReplaceAll(c.Detail, "|", "\\|")
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Change, what, c.Where, tested)
		}
		b.WriteString("\n")
	}
	for _, n := range res.Notes {
		b.WriteString("_Note: " + n + "_\n\n")
	}
	if res.Untested > 0 {
		fmt.Fprintf(&b, "**%s new %s no test yet.**\n", count(res.Untested, "decision or scenario"), map[bool]string{true: "has", false: "have"}[res.Untested == 1])
	} else {
		b.WriteString("Every new decision and scenario is covered by a test.\n")
	}
	return b.String()
}
