package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scaffold"
)

type scaffoldOptions struct {
	measure  measureOptions
	top      int
	out      string
	workflow string
}

func newScaffoldCommand() *cobra.Command {
	var opt scaffoldOptions
	cmd := &cobra.Command{
		Use:   "scaffold [<item-id>] [folder|folder/...]",
		Short: "Write a Go test skeleton for a test worth writing first",
		Example: "  pathkit scaffold sc:3fa94c0d17\n" +
			"  pathkit scaffold --top 3 --out ./orders",
		Long: `Writes a Go test (using go.temporal.io/sdk/testsuite) for one item of the
"Tests to write first" list: the test environment, the workflow registered,
the activities and child workflows mocked with the outcome the scenario needs
(a success with zero values, or a failure of the kind it needs), signals and a
cancel sent after a short delay, an input that takes the decisions of the
scenario when PathKit can work it out, and the assertions. What PathKit cannot
work out is a TODO line in the code.

Give an item id (the "id" of "pathkit report" and "pathkit report --json", like
sc:3fa94c0d17), or --top N for the first N items. The test is printed; with --out
<folder> it is written to pathkit_scaffold_<workflow>_test.go in that folder
(the folder of the workflow's package, so the test can see the workflow). An
existing file is never overwritten.

Scenarios are counted from the traces "pathkit test" recorded; without traces
every missed scenario is a candidate.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error { return runScaffold(cmd, args, opt) },
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.IntVar(&opt.top, "top", 0, "write the first `N` items of the list instead of one id")
	f.StringVar(&opt.out, "out", "", "write the test files to this `folder` instead of printing them (never overwrites)")
	f.StringVar(&opt.workflow, "workflow", "", "the `workflow` the item id belongs to, when the same id fits several")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	addScopeFlags(cmd, &opt.measure.scope, false)
	return cmd
}

func runScaffold(cmd *cobra.Command, args []string, opt scaffoldOptions) error {
	id, target := "", ""
	switch {
	case opt.top > 0:
		if len(args) > 0 {
			target = args[0]
		}
	case len(args) == 0:
		return userError("missing <item-id> (like sc:3fa94c0d17; \"pathkit report\" lists them in \"Tests to write first\") or --top N")
	default:
		id = args[0]
		if len(args) == 2 {
			target = args[1]
		}
	}
	opt.measure.scope.everyPackage = true
	opt.measure.tracesOptional = true
	opt.measure.noCI = true
	m, err := measure(cmd, "scaffold", target, opt.measure)
	if err != nil {
		return err
	}
	all := advice.TestsToWriteFirst(m.res, load.DisplayPath, 0)
	var picked []advice.Item
	if opt.top > 0 {
		for _, it := range all {
			if strings.HasPrefix(it.ID, "hd:") {
				continue // a handler: no scenario to write a scaffold for
			}
			picked = append(picked, it)
			if len(picked) == opt.top {
				break
			}
		}
		if len(picked) == 0 {
			return userError("nothing to write: no missed scenario is left")
		}
	} else {
		for _, it := range all {
			if it.ID == id && (opt.workflow == "" || it.Workflow == opt.workflow) {
				picked = append(picked, it)
			}
		}
		switch len(picked) {
		case 0:
			return userError("no item %q in the list of tests to write first (\"pathkit report\" prints it); it may already be covered, or the id belongs to another folder", id)
		case 1:
		default:
			var ws []string
			for _, it := range picked {
				ws = append(ws, it.Workflow)
			}
			return userError("the id %s fits %d workflows (%s); pick one with --workflow", id, len(picked), strings.Join(ws, ", "))
		}
		if strings.HasPrefix(picked[0].ID, "hd:") {
			return userError("%s is a handler no test used, which has no scenario to write a test from; send the signal or update it handles from a test of %s", id, picked[0].Workflow)
		}
	}
	graphs := map[string]*model.Graph{}
	for _, w := range m.workflows {
		graphs[w.Name] = w.Graph
	}
	paths := map[string]map[int]model.Path{}
	for _, w := range m.res.Workflows {
		paths[w.Name] = map[int]model.Path{}
		for _, p := range w.Paths {
			paths[w.Name][p.Number] = p.Path
		}
	}
	byWorkflow := map[string][]scaffold.Test{}
	var order []string
	for _, it := range picked {
		g := graphs[it.Workflow]
		p, ok := paths[it.Workflow][it.PathNumber]
		if g == nil || !ok {
			return userError("%s: PathKit has no path to write the test from", it.Workflow)
		}
		t, err := scaffold.Generate(it, g, p)
		if err != nil {
			return userError("%v", err)
		}
		if _, seen := byWorkflow[it.Workflow]; !seen {
			order = append(order, it.Workflow)
		}
		byWorkflow[it.Workflow] = append(byWorkflow[it.Workflow], t)
	}
	sort.Strings(order)
	type file struct {
		name  string
		tests []scaffold.Test
	}
	var files []file
	for _, wf := range order {
		if _, err := scaffold.Source(byWorkflow[wf]); err != nil && len(byWorkflow[wf]) > 1 {
			// the tests need different import names: one file for each
			for _, t := range byWorkflow[wf] {
				files = append(files, file{strings.TrimSuffix(scaffold.File(wf), "_test.go") + "_" + t.Name + "_test.go", []scaffold.Test{t}})
			}
			continue
		}
		files = append(files, file{scaffold.File(wf), byWorkflow[wf]})
	}
	stdout := cmd.OutOrStdout()
	for _, f := range files {
		src, err := scaffold.Source(f.tests)
		if err != nil {
			return userError("%v", err)
		}
		name := f.name
		if opt.out == "" {
			if len(files) > 1 {
				fmt.Fprintf(stdout, "// ---- %s ----\n", name)
			}
			fmt.Fprint(stdout, string(src))
			continue
		}
		if err := os.MkdirAll(opt.out, 0o755); err != nil {
			return userError("could not create %s: %v", opt.out, err)
		}
		path := filepath.Join(opt.out, name)
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				return userError("%s already exists; PathKit does not overwrite it (move it away, or print the test without --out)", path)
			}
			return userError("could not write %s: %v", path, err)
		}
		if _, err := fh.Write(src); err != nil {
			_ = fh.Close()
			return userError("could not write %s: %v", path, err)
		}
		if err := fh.Close(); err != nil {
			return userError("could not write %s: %v", path, err)
		}
		names := make([]string, 0, len(f.tests))
		for _, t := range f.tests {
			names = append(names, t.Name)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "pathkit scaffold: wrote %s (%s)\n", path, strings.Join(names, ", "))
	}
	return nil
}
