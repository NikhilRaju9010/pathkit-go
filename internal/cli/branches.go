package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type branchesOptions struct {
	measure   measureOptions
	workflow  string
	missed    bool
	json      bool
	technical bool
}

func newBranchesCommand() *cobra.Command {
	var opt branchesOptions
	cmd := &cobra.Command{
		Use:     "branches [folder|folder/...]",
		Short:   "Show branch coverage: how many decision outcomes your tests took",
		Example: "  pathkit branches\n  pathkit branches --missed\n  pathkit branches --workflow orders.OrderWorkflow --json",
		Long: `Branch coverage is PathKit's headline number. A decision is an if, an
error check on an activity call, a switch, a loop or a wait; each way it can
go is an outcome. Branch coverage is the share of the outcomes that can
happen that your tests took. Outcomes PathKit proves cannot happen are left
out and listed ("can't happen"); outcomes blocked by a known bug stay in.

Prints the headline, then one row per workflow, worst first. With --missed it
lists every missed outcome with its place and a plain sentence.

The numbers come from the same measurement as "pathkit report".`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runBranches(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.StringVar(&opt.workflow, "workflow", "", "show only this `workflow`")
	f.BoolVar(&opt.missed, "missed", false, "list every missed outcome with its place and a plain sentence")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.technical, "technical", false, "use the technical names (exits, decision pairs) instead of plain words")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

func runBranches(cmd *cobra.Command, target string, opt branchesOptions) error {
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	opt.measure.function = opt.workflow
	opt.measure.noCI = true
	m, err := measure(cmd, "branches", target, opt.measure)
	if err != nil {
		return err
	}
	items := advice.MissedOutcomes(m.res, load.DisplayPath, 0)
	if opt.json {
		out := branchesJSON(m.res, items)
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), buf.String())
		return nil
	}
	var b strings.Builder
	if opt.technical {
		h := m.res.BranchHeadline()
		fmt.Fprintf(&b, "Branches (exits taken): %s · impossible outcomes left out: %d · blocked by known-defect: %d · grade %s\n", render.Ratio(h.Covered, h.Possible, nil, "no branches"), h.Impossible, h.Blocked, h.Grade)
		fmt.Fprintf(&b, "Decision pairs: %s · condition coverage: %s\n\n", render.Ratio(h.PairsCovered, h.PairsTotal, nil, "none"), render.Ratio(h.Conditions.Covered, h.Conditions.Total, nil, "none"))
		for _, r := range m.res.WorkflowRows() {
			fmt.Fprintf(&b, "%s  %d/%d  %s  missed %d\n", r.Name, r.Covered, r.Possible, r.Grade, r.Missed)
		}
	} else {
		lines := render.BranchHeadlineLines(m.res, items)
		b.WriteString(strings.Join(lines[:min(len(lines)-1, 3)], "\n") + "\n\n")
		b.WriteString(render.WorkflowTable(m.res))
	}
	if opt.missed {
		b.WriteString("\nMissed outcomes\n")
		b.WriteString(render.MissedOutcomesBlock(items))
	}
	next := "Next: pathkit next   (the outcomes to test first)"
	if len(items) == 0 {
		next = "Next: pathkit report --html   (the full page)"
	}
	b.WriteString("\n" + next + "\n")
	fmt.Fprint(cmd.OutOrStdout(), b.String())
	return nil
}

type jsonBranchesOut struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Tool          string                 `json:"tool"`
	BranchCov     *jsonBranchCoverage    `json:"branchCoverage"`
	Conditions    *jsonConditionCoverage `json:"conditionCoverage,omitempty"`
	Pairs         jsonCount              `json:"twoStepCombinations"`
	Workflows     []jsonBranchRow        `json:"workflows"`
	Missed        []jsonNextItem         `json:"missed"`
}

type jsonBranchRow struct {
	Name     string              `json:"name"`
	Coverage *jsonBranchCoverage `json:"branchCoverage"`
	Missed   int                 `json:"missed"`
	Pairs    jsonCount           `json:"twoStepCombinations"`
}

func branchesJSON(res coverage.Result, items []advice.OutcomeItem) jsonBranchesOut {
	out := jsonBranchesOut{SchemaVersion: 1, Tool: "pathkit-go", BranchCov: branchJSON(res.BranchCov, res.BranchSettings.Grades), Workflows: []jsonBranchRow{}, Missed: nextJSON(items)}
	out.Conditions, _ = conditionJSON(res.Conditions)
	h := res.BranchHeadline()
	out.Pairs = count1(h.PairsCovered, h.PairsTotal)
	byName := map[string]coverage.WorkflowResult{}
	for _, w := range res.Workflows {
		byName[w.Name] = w
	}
	for _, r := range res.WorkflowRows() {
		w := byName[r.Name]
		out.Workflows = append(out.Workflows, jsonBranchRow{Name: r.Name, Coverage: branchJSON(w.BranchCov, res.BranchSettings.Grades), Missed: r.Missed, Pairs: count1(r.PairsCovered, r.PairsTotal)})
	}
	return out
}
