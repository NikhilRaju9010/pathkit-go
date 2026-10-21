package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scaffold"
)

type nextOptions struct {
	measure  measureOptions
	top      int
	scaffold bool
	json     bool
}

type jsonNextItem struct {
	Rank        int    `json:"rank"`
	Branch      string `json:"branch"`
	Workflow    string `json:"workflow"`
	Risk        string `json:"risk"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Sentence    string `json:"sentence"`
	TestName    string `json:"testName"`
	Paths       int    `json:"paths"`
	Scenarios   int    `json:"scenarios"`
	PathNumber  int    `json:"pathNumber,omitempty"`
	Decision    string `json:"decision"`
	OutcomeName string `json:"outcome"`
}

func nextJSON(items []advice.OutcomeItem) []jsonNextItem {
	out := []jsonNextItem{}
	for _, it := range items {
		out = append(out, jsonNextItem{Rank: it.Rank, Branch: it.ID, Workflow: it.Workflow, Risk: advice.RiskWord(it.Risk), File: it.File, Line: it.Line, Sentence: it.Sentence,
			TestName: it.TestName, Paths: it.Paths, Scenarios: it.Scenarios, PathNumber: it.PathNumber, Decision: it.Decision, OutcomeName: it.Outcome})
	}
	return out
}

func newNextCommand() *cobra.Command {
	var opt nextOptions
	cmd := &cobra.Command{
		Use:     "next [folder|folder/...]",
		Short:   "Show the decision outcomes to test next, riskiest first",
		Example: "  pathkit next\n  pathkit next --top 5\n  pathkit next --scaffold",
		Long: `Lists the decision outcomes that can happen and that no test took,
riskiest first (cancel, undo, activity failure, continue-as-new, timer or
timeout, signal or update, plain logic, old version). Each item has its
place, a plain sentence of what never happened, a suggested test name and
how many paths and scenarios a test of it would cover.

With --scaffold, a test skeleton is printed for each item.`,
		Args: atMostOne("<folder>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runNext(cmd, target, opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.IntVar(&opt.top, "top", 10, "how many outcomes to show (0: all)")
	f.BoolVar(&opt.scaffold, "scaffold", false, "print a test skeleton for each outcome")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	addScopeFlags(cmd, &opt.measure.scope, true)
	return cmd
}

func runNext(cmd *cobra.Command, target string, opt nextOptions) error {
	opt.measure.scope.everyPackage = true
	opt.measure.foldersOnly = true
	opt.measure.noCI = true
	m, err := measure(cmd, "next", target, opt.measure)
	if err != nil {
		return err
	}
	items := advice.MissedOutcomes(m.res, load.DisplayPath, opt.top)
	if opt.json {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "next": nextJSON(items)}); err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), buf.String())
		return nil
	}
	var b strings.Builder
	b.WriteString(render.NextBlock(items, 0))
	if opt.scaffold {
		text, err := scaffoldForOutcomes(m, items)
		if err != nil {
			return err
		}
		b.WriteString("\n" + text)
	}
	hint := "Next: pathkit next --scaffold   (a test skeleton for each)"
	if opt.scaffold {
		hint = "Next: write the test, run pathkit test, then pathkit branches"
	}
	if len(items) == 0 {
		hint = "Next: pathkit branches   (the whole picture)"
	}
	b.WriteString("\n" + hint + "\n")
	fmt.Fprint(cmd.OutOrStdout(), b.String())
	return nil
}

// scaffoldForOutcomes writes a test skeleton for each outcome: the road is the
// path chosen for it (the least risky untested path through it).
func scaffoldForOutcomes(m *measured, items []advice.OutcomeItem) (string, error) {
	graphs := map[string]*model.Graph{}
	for _, w := range m.workflows {
		graphs[w.Name] = w.Graph
	}
	paths := map[string]map[int]coverage.PathResult{}
	for _, w := range m.res.Workflows {
		paths[w.Name] = map[int]coverage.PathResult{}
		for _, p := range w.Paths {
			paths[w.Name][p.Number] = p
		}
	}
	byWorkflow := map[string][]scaffold.Test{}
	var order []string
	var skipped []string
	for _, it := range items {
		p, ok := paths[it.Workflow][it.PathNumber]
		g := graphs[it.Workflow]
		if !ok || g == nil || it.PathNumber == 0 {
			skipped = append(skipped, fmt.Sprintf("// %s (%s:%d): no listed path goes through this outcome, so PathKit cannot write a skeleton for it", it.TestName, it.File, it.Line))
			continue
		}
		ai := advice.Item{ID: it.ID, Workflow: it.Workflow, Tag: it.Risk, File: it.File, Line: it.Line, Hint: it.Sentence, TestName: it.TestName, Paths: it.Paths, PathNumber: it.PathNumber, Variants: 1}
		t, err := scaffold.Generate(ai, g, p.Path)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("// %s: %v", it.TestName, err))
			continue
		}
		if _, seen := byWorkflow[it.Workflow]; !seen {
			order = append(order, it.Workflow)
		}
		byWorkflow[it.Workflow] = append(byWorkflow[it.Workflow], t)
	}
	sort.Strings(order)
	var b strings.Builder
	for _, wf := range order {
		src, err := scaffold.Source(byWorkflow[wf])
		if err != nil {
			for _, t := range byWorkflow[wf] {
				one, err2 := scaffold.Source([]scaffold.Test{t})
				if err2 != nil {
					return "", userError("%v", err2)
				}
				fmt.Fprintf(&b, "// ---- %s ----\n%s\n", strings.TrimSuffix(scaffold.File(wf), "_test.go")+"_"+t.Name+"_test.go", one)
			}
			continue
		}
		fmt.Fprintf(&b, "// ---- %s ----\n%s\n", scaffold.File(wf), src)
	}
	for _, s := range skipped {
		b.WriteString(s + "\n")
	}
	return b.String(), nil
}
