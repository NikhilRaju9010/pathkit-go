package cli

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// explain (path classification): for one path, found by its
// stable id, say in plain words which bucket it is in, which label, who
// decided (a detector, a config rule, a config override, a
// //pathkit:untestable comment, or nobody), and why. It uses the same
// measure as coverage and report, so it can never tell a different story.

type explainOptions struct {
	measure measureOptions
}

var stableIDArg = regexp.MustCompile(`^s1:[0-9a-f]{12}$`)

func newExplainCommand() *cobra.Command {
	var opt explainOptions
	cmd := &cobra.Command{
		Use:     "explain <stableId> [folder|folder/...]",
		Short:   "Say why one path got its bucket and label, and who decided",
		Example: "  pathkit explain s1:5179b1c99bbb\n  pathkit explain s1:5179b1c99bbb ./orders",
		Long: `Explains one path, found by its stable id (the "stableId" in
"pathkit coverage --json" or "report --json", like s1:5179b1c99bbb):
which workflow it belongs to, whether a test ran it, its bucket and
label, which detector, config rule, config override or
//pathkit:untestable comment decided, and why.

With no folder, explain looks in every entry of the config's "packages",
else ./... . A stable id is only unique inside one workflow; when it
fits paths of several workflows, each is explained (use --function to
pick one). Trace files are optional: without them every path shows as
not tested.`,
		Args: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				return userError("missing <stableId> argument; usage: pathkit explain <stableId> [folder] (the ids are listed by \"pathkit report --json\", field stableId)")
			case len(args) > 2:
				return userError("expected a <stableId> and at most one folder, got %d arguments", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 2 {
				target = args[1]
			}
			return runExplain(cmd, args[0], target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.StringVar(&opt.measure.function, "function", "", "look only in this `workflow`")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	addScopeFlags(cmd, &opt.measure.scope, false)
	return cmd
}

func runExplain(cmd *cobra.Command, id, target string, opt explainOptions) error {
	if !stableIDArg.MatchString(id) {
		return userError("%q is not a stable id (expected \"s1:\" and 12 hex digits, like s1:5179b1c99bbb); \"pathkit report --json\" lists them (stableId)", id)
	}
	opt.measure.scope.everyPackage = true
	opt.measure.tracesOptional = true
	opt.measure.noCI = true
	m, err := measure(cmd, "explain", target, opt.measure)
	if err != nil {
		return err
	}
	var found []string
	var truncated []string
	for _, w := range m.res.Workflows {
		if w.Truncated {
			truncated = append(truncated, w.Name)
		}
		for _, p := range w.Paths {
			if p.StableID == id {
				found = append(found, explainPath(m, w, p))
			}
		}
	}
	if len(found) == 0 {
		msg := fmt.Sprintf("no listed path has the stable id %s in %s; \"pathkit report --json\" lists the ids (stableId), and an id changes when the text of a decision on its path changes", id, count(len(m.res.Workflows), "measured workflow"))
		if n := len(m.res.Excluded); n > 0 {
			msg += fmt.Sprintf("; %s excluded from scope and not looked at", count(n, "workflow is"))
			msg = strings.Replace(msg, "workflows is excluded", "workflows are excluded", 1)
		}
		if len(truncated) > 0 {
			msg += fmt.Sprintf("; %s more paths than PathKit lists (2000 each), so the id may be beyond the path cap: %s", map[bool]string{true: "this workflow has", false: "these workflows have"}[len(truncated) == 1], strings.Join(truncated, ", "))
		}
		return userError("%s", msg)
	}
	if len(found) > 1 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s fits a path in %d workflows (a stable id is only unique inside one workflow; --function picks one):\n\n", id, len(found))
	}
	fmt.Fprint(cmd.OutOrStdout(), strings.Join(found, "\n"))
	return nil
}

// explainHeader is the first line of an explanation. The file is shown with
// forward slashes on every system, as the JSON shows it.
func explainHeader(w coverage.WorkflowResult, p coverage.PathResult) string {
	return fmt.Sprintf("%s is path %d of %s (%s)\n", p.StableID, p.Number, w.Name, filepath.ToSlash(w.File))
}

// explainPath is the plain-words explanation of one path.
func explainPath(m *measured, w coverage.WorkflowResult, p coverage.PathResult) string {
	var b strings.Builder
	line := func(name, format string, args ...any) {
		fmt.Fprintf(&b, "%-12s %s\n", name+":", fmt.Sprintf(format, args...))
	}
	b.WriteString(explainHeader(w, p))
	fmt.Fprintf(&b, "  %s\n\n", render.PathText(p.Path))

	switch {
	case len(m.tracePaths) == 0:
		line("Tested", "unknown: no trace files in %s (record runs with \"pathkit test\")", m.traceDir)
	case p.Covered && p.StaleTrace:
		line("Tested", "yes, by %s, at least one recorded for an older version of the code (--allow-stale)", count(p.Traces, "recorded run"))
	case p.Covered:
		line("Tested", "yes, by %s", count(p.Traces, "recorded run"))
	default:
		line("Tested", "no; no recorded run took this path")
	}

	c := p.Class
	// A path a test ran is always testable (a real run proves it possible),
	// whatever was claimed (the coverage guard). Then the claim is what
	// the Note talks about, and "Decided by" says the run decided.
	var claimed classify.Classification
	overruled := false
	if p.Covered && m.engine != nil {
		claimed = m.engine.Claim(w.Name, p.Path)
		overruled = claimed.Bucket != classify.Testable
	}

	bucketDef, _ := classify.Lookup(classify.KindBucket, string(c.Bucket))
	line("Bucket", "%s: %s", c.Bucket, bucketDef.Meaning)
	if c.Label == "" {
		line("Label", "none (an ordinary path)")
	} else if d, ok := labelDefinition(m, c.Label); ok {
		line("Label", "%s: %s", c.Label, d.Meaning)
	} else {
		line("Label", "%s (no definition was found)", c.Label)
	}
	if overruled {
		line("Decided by", "a test: a recorded run took this path, which proves it can happen, so it counts as testable whatever was claimed")
		line("Why", "A path a test ran is never hidden from coverage.")
	} else {
		line("Decided by", "%s", decidedBy(m, c))
		line("Why", "%s", c.Explanation)
		if len(c.ExtraReasons) > 0 {
			line("Also", "%s also applied to this path; the first reason found wins (config override, then comment, then rule, then detector)", strings.Join(c.ExtraReasons, ", "))
		}
	}
	if c.Label != "" {
		if d, ok := labelDefinition(m, c.Label); ok && d.WhatToDo != "" {
			line("What to do", "%s", d.WhatToDo)
		}
	}

	if overruled {
		who, what := decidedBy(m, claimed), strings.TrimSuffix(claimed.Explanation, ".")
		switch claimed.Bucket {
		case classify.CannotBeTested:
			line("Note", "%s said cannot-be-tested (%s), but a test ran this path, so that claim is wrong and should be fixed or removed", who, what)
		case classify.NeedsSpecialTest:
			line("Note", "%s said needs-special-test (%s); a test ran this path, which is what that asks for, so it counts as testable", who, what)
		default:
			line("Note", "%s said %s (%s); a test ran this path, which settles it: it can happen", who, claimed.Bucket, what)
		}
	}
	if m.engine == nil {
		config := ".pathkitrc.json"
		if m.sc.cfg != nil {
			config = m.sc.cfg.Path
		}
		line("Note", "classification is not enabled, so PathKit does not look for impossible paths and every path counts as testable (set \"enabled\": true under \"classification\" in %s)", config)
	}
	if render.WantsHint(p) {
		b.WriteString("To cover it:\n")
		for _, h := range render.HowToCover(p.Path) {
			b.WriteString("  - " + h + "\n")
		}
	}
	return b.String()
}

func labelDefinition(m *measured, name string) (classify.Definition, bool) {
	if m.engine != nil {
		return m.engine.Label(name)
	}
	return classify.Lookup(classify.KindLabel, name)
}

// decidedBy names who decided, in plain words.
func decidedBy(m *measured, c classify.Classification) string {
	config := ".pathkitrc.json"
	if m.sc.cfg != nil {
		config = m.sc.cfg.Path
	}
	switch c.Origin {
	case classify.OriginOverride:
		list := "mark_testable"
		if c.Bucket != classify.Testable {
			list = "mark_untestable"
		}
		return fmt.Sprintf("a person: classification.paths.%s in %s", list, config)
	case classify.OriginComment:
		return "a person: a //pathkit:untestable comment in the code"
	case classify.OriginRule:
		return fmt.Sprintf("a person: config rule %q in %s", c.RuleID, config)
	case classify.OriginUnexplained:
		return "PathKit itself: a claim was not trusted because this workflow has a trace that fits no path"
	}
	if c.Detector != "" {
		return fmt.Sprintf("detector %s (automatic, %s)", c.Detector, c.Confidence)
	}
	return "nobody: no detector, rule, override or comment applies, so the path is testable by default"
}
