package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

func newCleanCommand() *cobra.Command {
	var traceDir, olderThan, config string
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete recorded trace files (only *.trace.json files)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := scope.Load(config, cwd)
			if err != nil {
				return userError("%s", err)
			}
			dir := traceDirFor(cmd, traceDir, cfg)
			cutoff := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC) // no --older-than: everything
			if olderThan != "" {
				age, err := parseAge(olderThan)
				if err != nil {
					return userError("%v", err)
				}
				cutoff = time.Now().Add(-age)
			}
			n, err := trace.ClearOlderThan(dir, cutoff)
			if err != nil {
				return userError("%s", err)
			}
			if n == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "pathkit clean: nothing to clean in %s\n", dir)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pathkit clean: deleted %s from %s\n", count(n, "trace file"), dir)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&traceDir, "traces", defaultTraceDir, "trace folder (default: the config's \"traces\", else .pathkit/traces)")
	f.StringVar(&olderThan, "older-than", "", "delete only traces older than this `age`, e.g. 30m, 12h, 7d")
	f.StringVar(&config, "config", "", "use this config `file` instead of searching for .pathkitrc.json")
	return cmd
}

// parseAge reads an age like "30m", "12h" or "7d" (days).
func parseAge(s string) (time.Duration, error) {
	bad := fmt.Errorf("invalid --older-than value: %q (use a number with m, h or d, e.g. 7d)", s)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n <= 0 {
			return 0, bad
		}
		return time.Duration(n * 24 * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, bad
	}
	return d, nil
}
