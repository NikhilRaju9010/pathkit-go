package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// suggest: for each group of needs-review paths, draft a config rule. The
// reason is "TODO" on purpose: the config loader refuses it, so a draft never
// takes effect until a person has written why.

type suggestOptions struct {
	measure measureOptions
	json    bool
}

type suggestion struct {
	Workflow string   `json:"workflow"`
	Label    string   `json:"currentLabel"`
	Paths    int      `json:"paths"`
	StableID []string `json:"stableIds"`
	Why      string   `json:"why"`
	// Exact is true when the rule's "when" is the full list of decisions of
	// one path (the shared decisions of the group would also hit other paths).
	Exact bool           `json:"exactToOnePath"`
	Rule  map[string]any `json:"rule"`
}

func newSuggestCommand() *cobra.Command {
	var opt suggestOptions
	cmd := &cobra.Command{
		Use:     "suggest [folder|folder/...]",
		Short:   "Draft config rules for the needs-review paths (the reason is TODO until you write it)",
		Example: "  pathkit suggest\n  pathkit suggest --json",
		Long: `Looks at the paths that are needs-review and no test ran, groups them by
workflow and label, and prints a draft entry for the config's
classification.rules for each group.

Nothing is changed. Every draft has the reason "TODO", and the config loader
refuses "TODO", so a draft only takes effect after you replace it with the real
reason. The draft's label is TODO too (the config refuses an unknown label): write
known-defect if a bug blocks the path (it stays in needs-review), or a
cannot-be-tested label (for example manual) with "impossible": true if you
know the path can never happen.

A draft's "when" lists the decisions the group's paths all take. If that would
also hit a path outside the group, the draft is one rule per path with every
decision of that path. Check each draft with "pathkit rules check" before
you rely on it.`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runSuggest(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

func runSuggest(cmd *cobra.Command, target string, opt suggestOptions) error {
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	opt.measure.tracesOptional = true
	opt.measure.noCI = true
	m, err := measure(cmd, "suggest", target, opt.measure)
	if err != nil {
		return err
	}
	if m.engine == nil {
		return userError("classification is not enabled, so no path is needs-review (set \"enabled\": true under \"classification\" in the config)")
	}
	sugs := suggestRules(m.res)
	if opt.json {
		b, _ := json.MarshalIndent(map[string]any{"schemaVersion": 1, "suggestions": nonNil(sugs)}, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), suggestText(sugs))
	return nil
}

func nonNil(s []suggestion) []suggestion {
	if s == nil {
		return []suggestion{}
	}
	return s
}

var idUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// suggestRules groups the needs-review paths no test ran by workflow and
// label and drafts one rule for each group.
func suggestRules(res coverage.Result) []suggestion {
	var out []suggestion
	for _, w := range res.Workflows {
		groups := map[string][]coverage.PathResult{}
		var labels []string
		for _, p := range w.Paths {
			if p.Covered || p.Class.Bucket != classify.NeedsReview || p.Class.Label == classify.KnownDefect {
				continue
			}
			l := p.Class.Label
			if l == "" {
				l = "needs-review"
			}
			if _, ok := groups[l]; !ok {
				labels = append(labels, l)
			}
			groups[l] = append(groups[l], p)
		}
		sort.Strings(labels)
		for _, l := range labels {
			out = append(out, draft(w, l, groups[l])...)
		}
	}
	for i := range out {
		out[i].Rule["id"] = fmt.Sprintf("%s-%d", out[i].Rule["id"], i+1)
	}
	return out
}

func draft(w coverage.WorkflowResult, label string, ps []coverage.PathResult) []suggestion {
	base := idUnsafe.ReplaceAllString("review-"+w.Name+"-"+label, "-")
	mk := func(when []string, members []coverage.PathResult, exact bool) suggestion {
		var ids []string
		for _, p := range members {
			ids = append(ids, p.StableID)
		}
		return suggestion{Workflow: w.Name, Label: label, Paths: len(members), StableID: ids, Why: members[0].Class.Explanation, Exact: exact,
			Rule: map[string]any{"id": base, "workflow": w.Name, "label": "TODO", "when": when, "reason": "TODO"}}
	}
	// the decisions every path of the group takes
	common := map[string]bool{}
	for _, d := range ps[0].Path.Decisions() {
		common[d] = true
	}
	for _, p := range ps[1:] {
		has := map[string]bool{}
		for _, d := range p.Path.Decisions() {
			has[d] = true
		}
		for d := range common {
			if !has[d] {
				delete(common, d)
			}
		}
	}
	var when []string
	for _, d := range ps[0].Path.Decisions() { // keep the path's order
		if common[d] {
			when = append(when, d)
		}
	}
	if len(when) > 0 && hitsOnly(w, when, ps) {
		return []suggestion{mk(when, ps, false)}
	}
	var out []suggestion
	for _, p := range ps {
		out = append(out, mk(p.Path.Decisions(), []coverage.PathResult{p}, true))
	}
	return out
}

// hitsOnly says whether a rule with these "when" decisions hits exactly the
// paths of the group among the workflow's listed paths.
func hitsOnly(w coverage.WorkflowResult, when []string, group []coverage.PathResult) bool {
	in := map[string]bool{}
	for _, p := range group {
		in[p.StableID] = true
	}
	for _, p := range w.Paths {
		has := map[string]bool{}
		for _, d := range p.Path.Decisions() {
			has[d] = true
		}
		all := true
		for _, d := range when {
			all = all && has[d]
		}
		if all && !in[p.StableID] {
			return false
		}
	}
	return true
}

func suggestText(sugs []suggestion) string {
	if len(sugs) == 0 {
		return "Suggest: no needs-review path without a test, so there is nothing to draft (paths you already marked known-defect are not drafted again).\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Suggest: %s for classification.rules. Each has the reason \"TODO\", which the config refuses until you replace it.\n", count(len(sugs), "draft"))
	b.WriteString("Replace the label TODO: known-defect if a bug blocks the path (it stays needs-review), or a cannot-be-tested label (for example manual, with \"impossible\": true) if it can never happen.\n")
	for i, s := range sugs {
		fmt.Fprintf(&b, "\n%d. %s: %s currently %s (%s)\n", i+1, s.Workflow, count(s.Paths, "path"), s.Label, strings.TrimSuffix(s.Why, "."))
		if s.Exact {
			b.WriteString("   (one rule for this path alone: its decisions are not shared with the rest of the group)\n")
		}
		j, _ := json.MarshalIndent(s.Rule, "   ", "  ")
		fmt.Fprintf(&b, "   %s\n", j)
	}
	return b.String()
}
