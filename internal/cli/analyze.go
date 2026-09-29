package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/htmlreport"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type analyzeOptions struct {
	summary bool
	limit   string
	mermaid bool
	out     string
	html    string
	scope   scopeFlags
}

func newAnalyzeCommand() *cobra.Command {
	var opt analyzeOptions
	cmd := &cobra.Command{
		Use:   "analyze <file|folder|folder/...>",
		Short: "List every possible path through the workflows in a file or package",
		Args:  exactlyOne("<file>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(cmd, args[0], opt)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&opt.summary, "summary", false, "only print each workflow's name and total path count")
	f.StringVar(&opt.limit, "limit", "", "print at most `n` paths per workflow")
	f.BoolVar(&opt.mermaid, "mermaid", false, "print a Mermaid diagram instead of the path list")
	f.StringVar(&opt.out, "out", "", "also write exactly what was printed to this `file`")
	addHTMLFlag(cmd, &opt.html, defaultAnalysisHTML, "the path listing")
	addScopeFlags(cmd, &opt.scope, true)
	return cmd
}

func runAnalyze(cmd *cobra.Command, arg string, opt analyzeOptions) error {
	limit := 0
	if opt.limit != "" {
		n, err := strconv.Atoi(opt.limit)
		if err != nil || n <= 0 {
			return userError("invalid --limit value: %q (it must be a positive whole number)", opt.limit)
		}
		limit = n
	}
	stderr := cmd.ErrOrStderr()
	if err := htmlSpaceHint(cmd, opt.html, defaultAnalysisHTML, arg); err != nil {
		return err
	}
	if opt.summary && limit > 0 {
		fmt.Fprintln(stderr, "pathkit analyze: --limit ignored because --summary was passed.")
		limit = 0
	}

	sc, err := loadScoped("analyze", arg, opt.scope, stderr)
	if err != nil {
		return err
	}
	for _, n := range sc.notAnalyzable {
		fmt.Fprintf(stderr, "pathkit analyze: in scope but not analyzable: %s: %v %s\n", n.name, n.err, notAnalyzableHint)
	}
	if len(sc.mapped)+len(sc.notAnalyzable)+len(sc.excluded) == 0 {
		return userError("%s has no exported workflow functions to analyze", arg)
	}

	var blocks []string
	var pageWorkflows []htmlreport.AnalysisWorkflow
	for _, r := range sc.mapped {
		ps := r.graph.Paths(model.DefaultMaxPaths)
		// The HTML always has the full listing, whatever --summary or --limit say.
		pageWorkflows = append(pageWorkflows, htmlreport.AnalysisWorkflow{
			Name: r.wf.Name, File: load.DisplayPath(r.wf.Filename), AddedByConfig: r.addedByConfig, Paths: ps,
		})
		var block string
		if opt.mermaid {
			block = render.Mermaid(r.graph, ps, model.DefaultMaxPaths)
		} else {
			block = render.Text(r.graph, ps, model.DefaultMaxPaths, render.TextOptions{Summary: opt.summary, Limit: limit})
		}
		if r.addedByConfig { // D8: always visible which workflows a person added
			block = strings.Replace(block, "Workflow: "+r.wf.Name+"\n", "Workflow: "+r.wf.Name+" (added by config)\n", 1)
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		fmt.Fprint(cmd.OutOrStdout(), strings.TrimPrefix(excludedBlock(sc.excluded), "\n"))
		if len(sc.notAnalyzable) == 0 {
			return userError("no workflows in scope to analyze (every workflow is excluded)")
		}
		return userError("no workflows could be analyzed")
	}

	output := strings.Join(blocks, "\n") + excludedBlock(sc.excluded)
	fmt.Fprint(cmd.OutOrStdout(), output)
	if opt.out != "" {
		if err := os.WriteFile(opt.out, []byte(output), 0o644); err != nil {
			return userError("could not write --out file: %v", err)
		}
	}
	// Only the --html flag turns this on: the config's "html" is for report (D12).
	if path := htmlPath(cmd, opt.html, defaultAnalysisHTML, nil); path != "" {
		heading, _, _ := strings.Cut(strings.TrimPrefix(excludedBlock(sc.excluded), "\n"), "\n")
		page, err := htmlreport.Analysis(htmlreport.AnalysisInput{
			Header: header(now(), sc), Workflows: pageWorkflows, ExcludedHeading: heading, Excluded: sc.excluded,
		})
		if err != nil {
			return err
		}
		if err := writeHTMLFile(path, page); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "pathkit analyze: wrote %s\n", path)
	}
	return nil
}
