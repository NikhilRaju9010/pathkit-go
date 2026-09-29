package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// buildPathkit compiles the real binary into a temp folder, stamping in a
// known version, the same way the release build will.
func buildPathkit(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pathkit")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/NikhilRaju9010/pathkit-go/internal/version.Version=v0.0.0-test",
		"-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

// runBinary runs the binary and returns stdout, stderr and the real
// process exit code.
func runBinary(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %s: %v", bin, err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func TestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short (CI runs it)")
	}
	bin := buildPathkit(t)

	t.Run("--version", func(t *testing.T) {
		stdout, stderr, code := runBinary(t, bin, "--version")
		if stdout != "v0.0.0-test\n" || stderr != "" || code != 0 {
			t.Errorf("got stdout=%q stderr=%q code=%d", stdout, stderr, code)
		}
	})

	t.Run("analyze without file exits 1", func(t *testing.T) {
		stdout, stderr, code := runBinary(t, bin, "analyze")
		if stdout != "" || stderr != "pathkit analyze: missing <file> argument\n" || code != 1 {
			t.Errorf("got stdout=%q stderr=%q code=%d", stdout, stderr, code)
		}
	})
}

// ordersTraces writes traces for orders paths 1 and 2 (2 of 3 = 66.7%),
// with the current code's hash.
func ordersTraces(t *testing.T) string {
	t.Helper()
	res, err := load.Load("../../testdata/pilot/orders")
	if err != nil {
		t.Fatal(err)
	}
	wf := discover.Find(res.Packages)[0]
	hash := model.FunctionHash(wf.Pkg.Fset, wf.Func)
	dir := t.TempDir()
	for name, steps := range map[string][]string{"a": {"J1.true"}, "b": {"J1.false", "J2.failure"}} {
		data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
			"functionHash": hash, "status": "complete", "steps": steps})
		if err := os.WriteFile(filepath.Join(dir, name+".trace.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The exit codes of "pathkit coverage", from the real program: 0 fine,
// 1 any real error, 2 below --fail-under (CLAUDE.md D7, D9).
func TestBinaryCoverageExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short (CI runs it)")
	}
	bin := buildPathkit(t)
	file := "../../testdata/pilot/orders/orders.go"
	traces := ordersTraces(t)
	cfg := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(cfg, []byte(`{"failUnder": 90}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string // prefix
	}{
		{"above the threshold", []string{"--fail-under", "50"}, 0, ""},
		{"below the threshold", []string{"--fail-under", "70"}, 2, "pathkit coverage: coverage 66.7% is below --fail-under 70%\n"},
		{"looks equal at one decimal", []string{"--fail-under", "66.7"}, 2, "pathkit coverage: coverage 66.67% is below --fail-under 66.7%\n"},
		{"just above", []string{"--fail-under", "66.6"}, 0, ""},
		{"no threshold", nil, 0, ""},
		{"bad threshold", []string{"--fail-under", "abc"}, 1, "pathkit coverage: invalid --fail-under value: \"abc\""},
		{"threshold over 100", []string{"--fail-under", "101"}, 1, "pathkit coverage: invalid --fail-under value: \"101\""},
		{"no trace files", []string{"--traces", t.TempDir()}, 1, "pathkit coverage: no trace files found in "},
		{"config failUnder", []string{"--config", cfg}, 2, "pathkit coverage: coverage 66.7% is below failUnder in " + cfg + " 90%\n"},
		{"flag beats config", []string{"--config", cfg, "--fail-under", "10"}, 0, ""},
		{"not analyzable in scope", []string{"--fail-under", "10", "../../testdata/fixtures/rules/unsupported.go"}, 1, "pathkit coverage: in scope but not analyzable: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"coverage", "--traces", traces}, tt.args...)
			if !slices.Contains(tt.args, "../../testdata/fixtures/rules/unsupported.go") {
				args = append(args, file)
			}
			_, stderr, code := runBinary(t, bin, args...)
			if code != tt.code || !strings.HasPrefix(stderr, tt.stderr) || (tt.stderr == "" && stderr != "") {
				t.Errorf("exit %d, stderr %q; want exit %d, stderr starting %q", code, stderr, tt.code, tt.stderr)
			}
		})
	}
}
