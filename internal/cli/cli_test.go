package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/version"
)

// run calls Run in-process and returns what it printed and its exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestErrorsUseOneLineStyleAndExitCode1(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"analyze without file", []string{"analyze"},
			"pathkit analyze: missing <file> argument\n"},
		{"analyze with two files", []string{"analyze", "a.go", "b.go"},
			"pathkit analyze: expected one <file> argument, got 2\n"},
		{"analyze file not found", []string{"analyze", "x.go"},
			"pathkit analyze: Workflow file not found: x.go\n"},
		{"misspelled flag", []string{"analyze", "--sumary", "x.go"},
			"pathkit analyze: unknown flag: --sumary\n"},
		{"coverage without file", []string{"coverage"},
			"pathkit coverage: missing <file> argument\n"},
		{"coverage without traces", []string{"coverage", "x.go"},
			"pathkit coverage: missing required --traces <dir> argument\n"},
		{"coverage not implemented yet", []string{"coverage", "x.go", "--traces", "t"},
			"pathkit coverage: not implemented yet (planned for M6)\n"},
		{"report without dir", []string{"report"},
			"pathkit report: missing <dir> argument\n"},
		{"report without traces", []string{"report", "."},
			"pathkit report: missing required --traces <dir> argument\n"},
		{"report not implemented yet", []string{"report", ".", "--traces=t"},
			"pathkit report: not implemented yet (planned for M7)\n"},
		{"unknown command", []string{"analyse"},
			"pathkit: unknown command \"analyse\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := run(t, tt.args...)
			if code != ExitError {
				t.Errorf("exit code = %d, want %d", code, ExitError)
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
		})
	}
}

func TestSummaryFlagAfterFileIsParsed(t *testing.T) {
	// The standard "flag" package would stop at "x.go" and treat
	// "--summary" as a second file; cobra must read it as a flag.
	cmd := newAnalyzeCommand()
	if err := cmd.ParseFlags([]string{"x.go", "--summary"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got, _ := cmd.Flags().GetBool("summary"); !got {
		t.Error("--summary written after the file was not read")
	}
	if args := cmd.Flags().Args(); len(args) != 1 || args[0] != "x.go" {
		t.Errorf("positional args = %v, want [x.go]", args)
	}
}

func TestVersionPrintsBareVersion(t *testing.T) {
	old := version.Version
	version.Version = "v9.9.9"
	t.Cleanup(func() { version.Version = old })

	stdout, stderr, code := run(t, "--version")
	if code != ExitOK || stdout != "v9.9.9\n" || stderr != "" {
		t.Errorf("got stdout=%q stderr=%q code=%d, want \"v9.9.9\\n\", \"\", 0", stdout, stderr, code)
	}
}

func TestNoArgumentsPrintsHelp(t *testing.T) {
	stdout, stderr, code := run(t)
	if code != ExitOK || stderr != "" || !bytes.Contains([]byte(stdout), []byte("analyze")) {
		t.Errorf("got stdout=%q stderr=%q code=%d, want help on stdout and exit 0", stdout, stderr, code)
	}
}

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"no error", nil, ExitOK},
		{"user error", userError("bad"), ExitError},
		{"below threshold", belowThresholdError("coverage 50.0%% is below --fail-under 80%%"), ExitBelowThreshold},
		{"plain error", errors.New("unknown flag: --x"), ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeFor(tt.err); got != tt.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	if got := formatError("report", belowThresholdError("coverage 62.5%% is below --fail-under 80%%")); got != "pathkit report: coverage 62.5% is below --fail-under 80%\n" {
		t.Errorf("got %q", got)
	}
	multi := errors.New("unknown command \"x\" for \"pathkit\"\n\nDid you mean this?\n\tanalyze\n")
	if got := formatError("", multi); got != "pathkit: unknown command \"x\"\n" {
		t.Errorf("got %q", got)
	}
}
