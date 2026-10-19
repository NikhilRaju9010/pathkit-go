package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	var safe runnerOptions
	cmd := &cobra.Command{
		Use:     "test [folder | folder/...] [-- go test flags]",
		Short:   "Run your Go tests and record which workflow paths they take",
		Example: "  pathkit test ./...\n  pathkit test ./... -- -run TestOrder -count=1",
		Long: `Runs "go test" with a marked-up copy of your workflows swapped in
(go test -overlay), and writes one trace file per workflow run.
Your source files and go.mod are never changed.

Coverage is recorded only by "pathkit test"; plain "go test" records nothing.

Only workflows in scope are recorded (.pathkitrc.json, --include,
--exclude). Every test still runs: the scope changes what is counted,
never what is tested.

Everything after "--" is passed to go test, for example:
  pathkit test ./... -- -run TestOrder -count=1

--no-network runs the tests in a network namespace with no network (Linux,
through "unshare -rn"), so a test that reaches for a real service fails
instead of reaching it. If unshare is not available PathKit refuses; it never
runs the tests with the network on after you asked for it off.
--skip-file <file> skips the tests named in a file: one regular expression
per line, blank lines and lines starting with # are ignored.`,
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
			return runTest(cmd, target, goArgs, traceDir, keep, sf, safe)
		},
	}
	cmd.Flags().StringVar(&traceDir, "traces", defaultTraceDir, "folder to write trace files into (the config's \"traces\" sets another default)")
	cmd.Flags().BoolVar(&keep, "keep-traces", false, "keep old trace files instead of clearing them first")
	cmd.Flags().BoolVar(&safe.noNetwork, "no-network", false, "run the tests with no network (Linux: through unshare -rn); refuses if that is not available")
	cmd.Flags().StringVar(&safe.skipFile, "skip-file", "", "skip the tests named in this `file` (one regular expression per line, # for comments)")
	addScopeFlags(cmd, &sf, false)
	return cmd
}

// writeOverlay writes the marked-up copies of workflows into the overlay
// folder (the same for pathkit test and pathkit prepare) and returns the
// absolute path of overlay.json. absTraces is baked into the recorder.
func writeOverlay(workflows []recordable, absTraces string) (string, error) {
	targets := make([]instrument.Target, len(workflows))
	for i, r := range workflows {
		targets[i] = instrument.Target{Workflow: r.wf, Graph: r.graph}
	}
	res, err := instrument.Instrument(targets, absTraces)
	if err != nil {
		return "", userError("%s", err)
	}
	if cwd, err := os.Getwd(); err == nil {
		if err := instrument.CheckOverlayDir(overlayDir, instrument.ModuleRoot(cwd)); err != nil {
			return "", userError("%s", err)
		}
	}
	overlay, err := instrument.WriteOverlay(res, overlayDir)
	if err != nil {
		return "", userError("could not write the overlay: %v", err)
	}
	return filepath.Abs(overlay)
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

func runTest(cmd *cobra.Command, target string, goArgs []string, traceDir string, keep bool, sf scopeFlags, safe runnerOptions) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	goArgs, err := safe.apply(goArgs)
	if err != nil {
		return err
	}
	goBin, goPrefix, err := safe.command()
	if err != nil {
		return err
	}
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
		overlay, err := writeOverlay(workflows, absTraces)
		if err != nil {
			return err
		}
		goTest = append(goTest, "-overlay="+overlay)
	} else {
		fmt.Fprintln(stderr, "pathkit test: no workflows in scope to record (no exported workflow functions found, or the scope leaves none); running the tests without recording")
	}
	goTest = append(goTest, pattern)
	// Cached test results would skip running the tests, so no traces would
	// be written. Any -count flag turns Go's test cache off; add -count=1
	// unless the user passed their own -count.
	if !hasCountFlag(goArgs) {
		goTest = append(goTest, "-count=1")
	}
	goTest = append(goTest, goArgs...)

	// A folder that holds several Go modules is tested module by module; one
	// overlay and one trace folder serve them all.
	dirs := []string{dir}
	if len(sc.res.Modules) > 0 {
		dirs = sc.res.Modules
	}
	var testErr error
	for _, d := range dirs {
		args := goTest
		if len(sc.res.Modules) > 0 {
			args = append([]string(nil), goTest...)
			for i, a := range args {
				if a == pattern {
					args[i] = "./..."
				}
			}
			fmt.Fprintf(stderr, "pathkit test: running the tests of the module in %s\n", load.DisplayPath(d))
		}
		run := exec.Command(goBin, append(append([]string(nil), goPrefix...), args...)...)
		run.Dir = d
		run.Stdout, run.Stderr = stdout, stderr
		run.Env = os.Environ()
		// A folder reached through a symbolic link (macOS temp folders are one)
		// keeps the link's name in $PWD, and "go test" would take that as the
		// working folder; the Go tool names files by their real path, so the
		// overlay would match nothing and no trace would be written. Name the
		// real folder. (Not on Windows: $PWD does not work that way there.)
		if runtime.GOOS != "windows" {
			if abs, err := filepath.Abs(d); err == nil {
				if real, err := filepath.EvalSymlinks(abs); err == nil {
					run.Env = append(run.Env, "PWD="+real)
				}
			}
		}
		if err := run.Run(); err != nil && testErr == nil {
			testErr = err
		}
	}

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
			return userError("go test failed (%v); traces recorded so far are kept. Fix the failing tests, then run \"pathkit test\" again", exitErr)
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
