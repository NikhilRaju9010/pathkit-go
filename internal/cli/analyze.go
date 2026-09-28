package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type analyzeOptions struct {
	summary bool
	limit   string
	mermaid bool
	out     string
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
	if opt.summary && limit > 0 {
		fmt.Fprintln(stderr, "pathkit analyze: --limit ignored because --summary was passed.")
		limit = 0
	}

	res, err := load.Load(arg)
	if err != nil {
		return userError("%s", err)
	}
	var workflows []discover.Workflow
	for _, wf := range discover.Find(res.Packages) {
		if res.File == "" || load.SameFile(wf.Filename, res.File) {
			workflows = append(workflows, wf)
		}
	}
	if len(workflows) == 0 {
		return userError("%s has no exported workflow functions to analyze", arg)
	}

	var blocks []string
	for _, wf := range workflows {
		g, err := model.Build(wf)
		var unsupported *model.UnsupportedError
		if errors.As(err, &unsupported) {
			fmt.Fprintf(stderr, "pathkit analyze: skipping %s: %v\n", wf.Name, err)
			continue
		}
		if err != nil {
			return err
		}
		ps := g.Paths(model.DefaultMaxPaths)
		if opt.mermaid {
			blocks = append(blocks, render.Mermaid(g, ps, model.DefaultMaxPaths))
		} else {
			blocks = append(blocks, render.Text(g, ps, model.DefaultMaxPaths, render.TextOptions{Summary: opt.summary, Limit: limit}))
		}
	}
	if len(blocks) == 0 {
		return userError("no workflows could be analyzed")
	}

	output := strings.Join(blocks, "\n")
	fmt.Fprint(cmd.OutOrStdout(), output)
	if opt.out != "" {
		if err := os.WriteFile(opt.out, []byte(output), 0o644); err != nil {
			return userError("could not write --out file: %v", err)
		}
	}
	return nil
}
