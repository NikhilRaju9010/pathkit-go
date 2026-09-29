package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/load"
)

// pathkit prepare (CLAUDE.md D4): for people who run "go test" themselves.
// It writes the same overlay "pathkit test" uses and prints the go test
// command that records traces with it.
func newPrepareCommand() *cobra.Command {
	var traceDir string
	var sf scopeFlags
	cmd := &cobra.Command{
		Use:   "prepare [folder | folder/...]",
		Short: "Write the overlay and print the go test command that records traces",
		Long: `Writes the marked-up copies of your in-scope workflows to .pathkit/overlay
and prints the "go test -overlay=..." command to run. Only that command
records traces; a plain "go test" records nothing. Run prepare again after
you change a workflow.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			switch len(args) {
			case 0:
			case 1:
				target = args[0]
			default:
				return userError("expected at most one folder argument, got %d", len(args))
			}
			return runPrepare(cmd, target, traceDir, sf)
		},
	}
	cmd.Flags().StringVar(&traceDir, "traces", defaultTraceDir, "folder the recorded traces go to (default: the config's \"traces\", else .pathkit/traces)")
	addScopeFlags(cmd, &sf, false)
	return cmd
}

func runPrepare(cmd *cobra.Command, target, traceDir string, sf scopeFlags) error {
	stderr := cmd.ErrOrStderr()
	if target != "" {
		if _, _, file, err := load.Resolve(target); err != nil {
			return userError("%s", err)
		} else if file != "" {
			return userError("expected a package folder or folder/..., not a file: %s", target)
		}
	}
	sc, err := loadScoped("prepare", target, sf, stderr)
	if err != nil {
		return err
	}
	for _, e := range sc.excluded {
		fmt.Fprintf(stderr, "pathkit prepare: not recording %s: excluded from scope (%s)\n", e.Name, e.Reason)
	}
	for _, n := range sc.notAnalyzable {
		fmt.Fprintf(stderr, "pathkit prepare: not recording %s: in scope but not analyzable: %v %s\n", n.name, n.err, notAnalyzableHint)
	}
	if len(sc.mapped) == 0 {
		return userError("no workflows in scope to record")
	}
	absTraces, err := filepath.Abs(traceDirFor(cmd, traceDir, sc.cfg))
	if err != nil {
		return err
	}
	overlay, err := writeOverlay(sc.mapped, absTraces)
	if err != nil {
		return err
	}
	dir, pattern, _, err := load.Resolve(sc.target)
	if err != nil {
		return userError("%s", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "go test -overlay=%s -count=1 %s\n", overlay, pattern)
	if cwd, _ := os.Getwd(); !sameDir(cwd, dir) {
		fmt.Fprintf(stderr, "pathkit prepare: run it in %s\n", dir)
	}
	fmt.Fprintf(stderr, "pathkit prepare: traces go to %s. A plain \"go test\" without -overlay records nothing; run prepare again after you change a workflow.\n", absTraces)
	return nil
}

func sameDir(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}
