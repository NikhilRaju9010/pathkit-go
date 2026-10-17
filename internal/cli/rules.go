package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
)

// rules check: for every rule, override, //pathkit:untestable comment and
// explained trace, how many paths (or traces) it hits right now. It reads the
// same measure as report and explain.
//
// A rule or mark that says a path is impossible (or blocked by a bug) and
// hits a path a test ran is refused: a run proves the path is possible, so
// the entry is wrong. An entry that hits nothing, and two entries that hit the
// same path, are warnings.

type rulesCheckOptions struct {
	measure measureOptions
	json    bool
}

func newRulesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Check the rules, overrides and explained traces in the config",
	}
	cmd.AddCommand(newRulesCheckCommand())
	return cmd
}

func newRulesCheckCommand() *cobra.Command {
	var opt rulesCheckOptions
	cmd := &cobra.Command{
		Use:     "check [folder|folder/...]",
		Short:   "Show how many paths each rule, override and explained trace hits, and refuse the ones a test disproves",
		Example: "  pathkit rules check\n  pathkit rules check --json",
		Long: `Lists every entry in the config's classification section (rules, mark_untestable
and mark_testable overrides, explained_traces) and every //pathkit:untestable
comment, with how many paths (or traces) it hits in the current code.

Exit 1 (refused) when a rule, an untestable override or a comment says a
path is impossible, or blocked by a known bug, and a test ran that path: a
recorded run proves the path can happen, so the entry is wrong. Remove it, or
fix the condition it names.

Warnings (exit 0): an entry that hits no listed path, and two entries that hit
the same path (the first reason found wins: override, then comment, then rule).

Trace files are optional; without them nothing counts as tested, so nothing
is refused. Classification must be enabled in the config.`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runRulesCheck(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

type entryHit struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Workflow string   `json:"workflow"`
	Label    string   `json:"label,omitempty"`
	Reason   string   `json:"reason"`
	Hits     int      `json:"hits"`
	Covered  int      `json:"covered"`
	Overlaps []string `json:"overlapsWith,omitempty"`
	// TracesHit is for an explained trace: the traces it explains.
	Problems []string `json:"problems,omitempty"`
}

type rulesCheckResult struct {
	SchemaVersion int        `json:"schemaVersion"`
	Entries       []entryHit `json:"entries"`
	Refused       []string   `json:"refused"`
	Warnings      []string   `json:"warnings"`
}

func runRulesCheck(cmd *cobra.Command, target string, opt rulesCheckOptions) error {
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	opt.measure.tracesOptional = true
	opt.measure.noCI = true
	m, err := measure(cmd, "rules check", target, opt.measure)
	if err != nil {
		return err
	}
	if m.engine == nil {
		return userError("classification is not enabled, so there are no rules to check (set \"enabled\": true under \"classification\" in the config)")
	}
	r := checkEntries(m)
	if opt.json || (m.sc.cfg != nil && m.sc.cfg.JSON && !cmd.Flags().Changed("json")) {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	} else {
		fmt.Fprint(cmd.OutOrStdout(), rulesCheckText(r))
	}
	if len(r.Refused) > 0 {
		return userError("%s refused: a test ran the path, so the claim is wrong", count(len(r.Refused), "entry"))
	}
	return nil
}

func checkEntries(m *measured) rulesCheckResult {
	res := rulesCheckResult{SchemaVersion: 1, Entries: []entryHit{}, Refused: []string{}, Warnings: []string{}}
	var names []string
	truncated := map[string]bool{}
	for _, w := range m.res.Workflows {
		names = append(names, w.Name)
		truncated[w.Name] = w.Truncated || w.Capped
	}
	entries := m.engine.Entries(names)
	for _, en := range entries {
		res.Entries = append(res.Entries, entryHit{Kind: en.Kind, ID: en.ID, Workflow: en.Workflow, Label: en.Label, Reason: en.Reason})
	}
	idx := map[string]int{}
	for i, en := range entries {
		idx[en.Key()] = i
	}
	for _, w := range m.res.Workflows {
		for _, p := range w.Paths {
			hits := m.engine.EntriesHitting(w.Name, p.Path)
			for _, en := range hits {
				h := &res.Entries[idx[en.Key()]]
				h.Hits++
				if p.Covered {
					h.Covered++
					if en.Claims != classify.Testable {
						msg := fmt.Sprintf("%s %s (%s) says path %d (%s) of %s is %s, but %s ran it", en.Kind, quoteID(en), en.Label, p.Number, p.StableID, w.Name, en.Claims, count(p.Traces, "recorded run"))
						h.Problems = append(h.Problems, msg)
						res.Refused = append(res.Refused, msg)
					}
				}
				if len(hits) > 1 {
					for _, o := range hits {
						if o.Key() != en.Key() {
							h.Overlaps = appendOnce(h.Overlaps, fmt.Sprintf("%s %s", o.Kind, quoteID(o)))
						}
					}
				}
			}
		}
	}
	for i := range res.Entries {
		h, en := &res.Entries[i], entries[i]
		switch {
		case h.Hits == 0 && truncated[en.Workflow]:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s %s hits none of the listed paths of %s; the workflow has more paths than the list holds, so it may hit a path that is not listed", en.Kind, quoteID(en), en.Workflow))
		case h.Hits == 0:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s %s hits no path of %s: it is stale (the code changed, or the text is mistyped; write decisions as \"pathkit analyze\" prints them)", en.Kind, quoteID(en), en.Workflow))
		}
		sort.Strings(h.Overlaps)
		for _, o := range h.Overlaps {
			me := fmt.Sprintf("%s %s", en.Kind, quoteID(en))
			if me < o { // each pair once
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s and %s hit the same path (the reason found first wins: override, then comment, then rule)", me, o))
			}
		}
	}
	// explained traces
	if m.sc.cfg != nil {
		unmatched := map[string]int{}
		for _, w := range m.res.Workflows {
			for _, u := range w.Unmatched {
				unmatched[w.Name+"\x00"+u.StepsID] += u.Count
			}
		}
		for _, x := range m.sc.cfg.Classification.Explained {
			if !contains(names, x.Workflow) {
				continue
			}
			n := unmatched[x.Workflow+"\x00"+x.StepsID]
			h := entryHit{Kind: "explained-trace", ID: x.StepsID, Workflow: x.Workflow, Reason: x.Reason, Hits: n}
			if n == 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("explained trace %s hits no trace of %s: no recorded run leaves the map like this any more; remove the entry", x.StepsID, x.Workflow))
			}
			res.Entries = append(res.Entries, h)
		}
	}
	return res
}

func quoteID(en classify.Entry) string {
	if en.Kind == classify.EntryRule {
		return fmt.Sprintf("%q", en.ID)
	}
	return en.ID
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func rulesCheckText(r rulesCheckResult) string {
	var b strings.Builder
	if len(r.Entries) == 0 {
		return "Rules check: no rules, overrides, comments or explained traces for the workflows in scope.\n"
	}
	fmt.Fprintf(&b, "Rules check: %s\n", count(len(r.Entries), "entry"))
	for _, h := range r.Entries {
		label := ""
		if h.Label != "" {
			label = " [" + h.Label + "]"
		}
		what := count(h.Hits, "path")
		if h.Kind == "explained-trace" {
			what = count(h.Hits, "trace")
		}
		id := h.ID
		if h.Kind == classify.EntryRule {
			id = fmt.Sprintf("%q", h.ID)
		}
		fmt.Fprintf(&b, "  %s %s%s in %s: hits %s", h.Kind, id, label, h.Workflow, what)
		if h.Covered > 0 {
			fmt.Fprintf(&b, ", %d of them run by a test", h.Covered)
		}
		b.WriteString("\n")
	}
	for _, s := range r.Refused {
		fmt.Fprintf(&b, "REFUSED: %s\n", s)
	}
	for _, s := range r.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", s)
	}
	if len(r.Refused) == 0 && len(r.Warnings) == 0 {
		b.WriteString("All entries hit paths no test has run, and none overlap.\n")
	}
	return b.String()
}
