package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/instrument"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

const (
	defaultTraceDir = ".pathkit/traces"
	overlayDir      = ".pathkit/overlay"
)

func newTestCommand() *cobra.Command {
	var traceDir string
	var keep bool
	var sf scopeFlags
	cmd := &cobra.Command{
		Use:   "test [folder | folder/...] [-- go test flags]",
		Short: "Run your Go tests and record which workflow paths they take",
		Long: `Runs "go test" with a marked-up copy of your workflows swapped in
(go test -overlay), and writes one trace file per workflow run.
Your source files and go.mod are never changed.

Coverage is recorded only by "pathkit test"; plain "go test" records nothing.

Only workflows in scope are recorded (.pathkitrc.json, --include,
--exclude). Every test still runs: the scope changes what is counted,
never what is tested.

Everything after "--" is passed to go test, for example:
  pathkit test ./... -- -run TestOrder -count=1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, goArgs := "", []string{} // "": the config's package, else ./...
			if dash := cmd.ArgsLenAtDash(); dash >= 0 {
				goArgs = args[dash:]
				args = args[:dash]
			}
			switch len(args) {
			case 0:
			case 1:
				target = args[0]
			default:
				return userError("expected at most one folder argument, got %d", len(args))
			}
			return runTest(cmd, target, goArgs, traceDir, keep, sf)
		},
	}
	cmd.Flags().StringVar(&traceDir, "traces", defaultTraceDir, "folder to write trace files into (default: the config's \"traces\", else .pathkit/traces)")
	cmd.Flags().BoolVar(&keep, "keep-traces", false, "keep old trace files instead of clearing them first")
	addScopeFlags(cmd, &sf, false)
	return cmd
}

// traceDirFor is the trace folder a command uses: --traces when given,
// else the config's "traces" (relative to the config file), else the
// default.
func traceDirFor(cmd *cobra.Command, flagValue string, cfg *scope.Config) string {
	if !cmd.Flags().Changed("traces") && cfg != nil && cfg.Traces != "" {
		return cfg.Abs(cfg.Traces)
	}
	return flagValue
}

func runTest(cmd *cobra.Command, target string, goArgs []string, traceDir string, keep bool, sf scopeFlags) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if target != "" {
		if _, _, file, err := load.Resolve(target); err != nil {
			return userError("%s", err)
		} else if file != "" {
			return userError("expected a package folder or folder/..., not a file: %s", target)
		}
	}
	sc, err := loadScoped("test", target, sf, stderr)
	if err != nil {
		return err
	}
	dir, pattern, _, err := load.Resolve(sc.target)
	if err != nil {
		return userError("%s", err)
	}
	for _, e := range sc.excluded {
		fmt.Fprintf(stderr, "pathkit test: not recording %s: excluded from scope (%s)\n", e.Name, e.Reason)
	}
	for _, n := range sc.notAnalyzable {
		fmt.Fprintf(stderr, "pathkit test: not recording %s: in scope but not analyzable: %v %s\n", n.name, n.err, notAnalyzableHint)
	}
	workflows := sc.mapped
	traceDir = traceDirFor(cmd, traceDir, sc.cfg)

	absTraces, err := filepath.Abs(traceDir)
	if err != nil {
		return err
	}
	if !keep {
		if _, err := trace.Clear(absTraces); err != nil {
			return userError("%s", err)
		}
	}

	goTest := []string{"test"}
	if len(workflows) > 0 {
		targets := make([]instrument.Target, len(workflows))
		for i, r := range workflows {
			targets[i] = instrument.Target{Workflow: r.wf, Graph: r.graph}
		}
		res, err := instrument.Instrument(targets, absTraces)
		if err != nil {
			return userError("%s", err)
		}
		overlay, err := instrument.WriteOverlay(res, overlayDir)
		if err != nil {
			return userError("could not write the overlay: %v", err)
		}
		goTest = append(goTest, "-overlay="+overlay)
	} else {
		fmt.Fprintln(stderr, "pathkit test: no workflows in scope to record; running the tests without recording")
	}
	goTest = append(goTest, pattern)
	// Cached test results would skip running the tests, so no traces would
	// be written. Any -count flag turns Go's test cache off; add -count=1
	// unless the user passed their own -count.
	if !hasCountFlag(goArgs) {
		goTest = append(goTest, "-count=1")
	}
	goTest = append(goTest, goArgs...)

	run := exec.Command("go", goTest...)
	run.Dir = dir
	run.Stdout, run.Stderr = stdout, stderr
	run.Env = os.Environ()
	testErr := run.Run()

	files, err := trace.List(absTraces)
	if err != nil {
		return userError("%s", err)
	}
	complete := 0
	for _, f := range files {
		if t, err := trace.Read(f); err == nil && t.Status == "complete" {
			complete++
		}
	}
	fmt.Fprintf(stderr, "pathkit test: recorded %s (%d complete) in %s\n", count(len(files), "trace"), complete, traceDir)

	if testErr != nil {
		var exitErr *exec.ExitError
		if errors.As(testErr, &exitErr) {
			return userError("go test failed (%v)", exitErr)
		}
		return userError("could not run go test: %v", testErr)
	}
	return nil
}

// count prints "1 trace", "2 traces", "0 traces".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func hasCountFlag(args []string) bool {
	for _, a := range args {
		if a == "-count" || a == "--count" || strings.HasPrefix(a, "-count=") || strings.HasPrefix(a, "--count=") {
			return true
		}
	}
	return false
}
