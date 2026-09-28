package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/instrument"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

const (
	defaultTraceDir = ".pathkit/traces"
	overlayDir      = ".pathkit/overlay"
)

// recordable is a workflow PathKit can record, with its graph and hash.
type recordable struct {
	wf    discover.Workflow
	graph *model.Graph
	hash  string
}

// loadRecordable loads arg and returns the workflows PathKit can map. Each
// skipped workflow gets one stderr line starting with prefix.
func loadRecordable(arg, prefix string, stderr io.Writer) (*load.Result, []recordable, error) {
	res, err := load.Load(arg)
	if err != nil {
		return nil, nil, userError("%s", err)
	}
	var out []recordable
	for _, wf := range discover.Find(res.Packages) {
		if res.File != "" && !load.SameFile(wf.Filename, res.File) {
			continue
		}
		g, err := model.Build(wf)
		var u *model.UnsupportedError
		if errors.As(err, &u) {
			fmt.Fprintf(stderr, "%s%s: %v\n", prefix, wf.Name, err)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		out = append(out, recordable{wf: wf, graph: g, hash: model.FunctionHash(wf.Pkg.Fset, wf.Func)})
	}
	return res, out, nil
}

func newTestCommand() *cobra.Command {
	var traceDir string
	var keep bool
	cmd := &cobra.Command{
		Use:   "test [folder | folder/...] [-- go test flags]",
		Short: "Run your Go tests and record which workflow paths they take",
		Long: `Runs "go test" with a marked-up copy of your workflows swapped in
(go test -overlay), and writes one trace file per workflow run.
Your source files and go.mod are never changed.

Coverage is recorded only by "pathkit test"; plain "go test" records nothing.

Everything after "--" is passed to go test, for example:
  pathkit test ./... -- -run TestOrder -count=1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, goArgs := "./...", []string{}
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
			return runTest(cmd, target, goArgs, traceDir, keep)
		},
	}
	cmd.Flags().StringVar(&traceDir, "traces", defaultTraceDir, "folder to write trace files into")
	cmd.Flags().BoolVar(&keep, "keep-traces", false, "keep old trace files instead of clearing them first")
	return cmd
}

func runTest(cmd *cobra.Command, target string, goArgs []string, traceDir string, keep bool) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	dir, pattern, file, err := load.Resolve(target)
	if err != nil {
		return userError("%s", err)
	}
	if file != "" {
		return userError("expected a package folder or folder/..., not a file: %s", target)
	}
	_, workflows, err := loadRecordable(target, "pathkit test: not recording ", stderr)
	if err != nil {
		return err
	}

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
		fmt.Fprintln(stderr, "pathkit test: no recordable workflows found; running the tests without recording")
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
