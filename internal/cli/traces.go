package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

func newTracesCommand() *cobra.Command {
	var traceDir string
	var sf scopeFlags
	cmd := &cobra.Command{
		Use:   "traces [folder | folder/...]",
		Short: "Show which path each recorded trace took (a debug view; coverage arrives in M6)",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "" // the config's package, else ./...
			switch len(args) {
			case 0:
			case 1:
				target = args[0]
			default:
				return userError("expected at most one folder argument, got %d", len(args))
			}
			return runTraces(cmd, target, traceDir, sf)
		},
	}
	cmd.Flags().StringVar(&traceDir, "traces", defaultTraceDir, "folder the trace files are in (default: the config's \"traces\", else .pathkit/traces)")
	addScopeFlags(cmd, &sf, false)
	return cmd
}

func runTraces(cmd *cobra.Command, target, traceDir string, sf scopeFlags) error {
	sc, err := loadScoped("traces", target, sf, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	for _, n := range sc.notAnalyzable {
		fmt.Fprintf(cmd.ErrOrStderr(), "pathkit traces: in scope but not analyzable: %s: %v %s\n", n.name, n.err, notAnalyzableHint)
	}
	traceDir = traceDirFor(cmd, traceDir, sc.cfg)
	files, err := trace.List(traceDir)
	if err != nil {
		return userError("%s", err)
	}
	if len(files) == 0 {
		return userError("%s", trace.NoTracesMessage(traceDir))
	}
	byName := map[string]recordable{}
	for _, r := range sc.mapped {
		byName[r.wf.Name] = r
	}
	excluded := map[string]string{}
	for _, e := range sc.excluded {
		excluded[e.Name] = e.Reason
	}

	type line struct{ workflow, text string }
	var lines []line
	counts := map[trace.Kind]int{}
	for _, path := range files {
		f, err := trace.Read(path)
		if err != nil {
			lines = append(lines, line{"~", fmt.Sprintf("%s: could not read trace: %v", filepath.Base(path), err)})
			counts["unreadable"]++
			continue
		}
		if reason, out := excluded[f.Workflow]; out {
			counts[trace.Excluded]++
			lines = append(lines, line{f.Workflow, fmt.Sprintf("%s: excluded from scope (%s) [%s]", f.Workflow, reason, filepath.Base(path))})
			continue
		}
		r, ok := byName[f.Workflow]
		var out trace.Outcome
		if ok {
			out = trace.Check(f, r.graph, r.hash)
		} else {
			out = trace.Check(f, nil, "")
		}
		counts[out.Kind]++
		if out.Kind == trace.Matched {
			lines = append(lines, line{f.Workflow, fmt.Sprintf("%s: path %s (%s)", f.Workflow, pathNumber(r.graph, out.Path), describe(out.Path))})
		} else {
			lines = append(lines, line{f.Workflow, fmt.Sprintf("%s: %s: %s [%s]", f.Workflow, out.Kind, out.Reason, filepath.Base(path))})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].workflow != lines[j].workflow {
			return lines[i].workflow < lines[j].workflow
		}
		return lines[i].text < lines[j].text // "path 1" before "path 2"
	})

	w := cmd.OutOrStdout()
	for _, l := range lines {
		fmt.Fprintln(w, l.text)
	}
	var parts []string
	for _, k := range []trace.Kind{trace.Matched, trace.Unmatched, trace.Stale, trace.Incomplete, trace.Excluded, trace.UnknownWorkflow, "unreadable"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	fmt.Fprintf(w, "\n%s: %s\n", count(len(files), "trace"), strings.Join(parts, ", "))
	return nil
}

// pathNumber is the path's number in analyze's listing for this workflow.
func pathNumber(g *model.Graph, p model.Path) string {
	for i, q := range g.Paths(model.DefaultMaxPaths).List {
		if q.Key() == p.Key() {
			return fmt.Sprint(i + 1)
		}
	}
	return "(beyond the first 2000)"
}

// describe prints a path compactly: "J1.false J2.failure → End (failed)".
func describe(p model.Path) string {
	ids := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ids[i] = s.Exit.ID.String()
	}
	end := p.End.String()
	if p.Compensation {
		end += " " + model.CompensationNote
	}
	if len(ids) == 0 {
		return "no junctions → " + end
	}
	return strings.Join(ids, " ") + " → " + end
}
