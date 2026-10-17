package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
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
		if stdout != "" || stderr != "pathkit analyze: missing <file> argument; give a .go file, a folder, or folder/... (for example: pathkit analyze ./...)\n" || code != 1 {
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
		data, _ := json.Marshal(map[string]any{"schemaVersion": 2, "tool": "pathkit-go", "workflow": "orders.OrderWorkflow",
			"functionHash": hash, "status": "complete", "steps": steps})
		if err := os.WriteFile(filepath.Join(dir, name+".trace.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The exit codes of "pathkit coverage", from the real program: 0 fine,
// 1 any real error, 2 below --fail-under (docs/DESIGN.md D7, D9).
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

// The exit codes of "pathkit report", from the real program: 0 fine,
// 1 any real error (including an in-scope workflow PathKit can't analyze
// and a package that doesn't compile), 2 below --fail-under.
func TestBinaryReportExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short (CI runs it)")
	}
	bin := buildPathkit(t)
	orders := "../../testdata/pilot/orders"
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
		{"fine", []string{orders}, 0, ""},
		{"summary", []string{orders, "--summary"}, 0, ""},
		{"json", []string{orders, "--json"}, 0, ""},
		{"above the threshold", []string{orders, "--fail-under", "50"}, 0, ""},
		{"below the threshold", []string{orders, "--fail-under", "70"}, 2, "pathkit report: coverage 66.7% is below --fail-under 70%\n"},
		{"below, as JSON", []string{orders, "--json", "--fail-under", "70"}, 2, "pathkit report: coverage 66.7% is below --fail-under 70%\n"},
		{"config failUnder", []string{orders, "--config", cfg}, 2, "pathkit report: coverage 66.7% is below failUnder in " + cfg + " 90%\n"},
		{"flag beats config", []string{orders, "--config", cfg, "--fail-under", "10"}, 0, ""},
		{"bad threshold", []string{orders, "--fail-under", "abc"}, 1, "pathkit report: invalid --fail-under value: \"abc\""},
		{"no trace files", []string{orders, "--traces", t.TempDir()}, 1, "pathkit report: no trace files found in "},
		{"a single file", []string{orders + "/orders.go"}, 1, "pathkit report: expected a folder or folder/..., not a file: "},
		{"not analyzable in scope", []string{"../../testdata/fixtures/rules/...", "--fail-under", "10"}, 1, "pathkit report: in scope but not analyzable: "},
		{"a broken package", []string{"../../testdata/fixtures/...", "--fail-under", "10"}, 1, "pathkit report: package does not compile: example.com/fixtures/broken: "},
		{"unknown flag", []string{orders, "--sumary"}, 1, "pathkit report: unknown flag: --sumary\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"report", "--traces", traces}, tt.args...)
			stdout, stderr, code := runBinary(t, bin, args...)
			if code != tt.code || !strings.HasPrefix(stderr, tt.stderr) || (tt.stderr == "" && stderr != "") {
				t.Errorf("exit %d, stderr %q; want exit %d, stderr starting %q", code, stderr, tt.code, tt.stderr)
			}
			// Exit 2 still prints the whole report first.
			if code == 2 && !strings.Contains(stdout, "66.7") {
				t.Errorf("exit 2 without the report:\n%s", stdout)
			}
		})
	}
}

// report --html from the real program: exit 0 and exit 2 write the
// page and the trend, exit 1 writes nothing, and "--html out.html" (with a
// space) explains how to name the file.
func TestBinaryReportHTML(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short (CI runs it)")
	}
	bin := buildPathkit(t)
	orders := "../../testdata/pilot/orders"
	traces := ordersTraces(t)
	dir := t.TempDir()
	tests := []struct {
		name    string
		args    []string
		code    int
		written bool
		stderr  string // substring
	}{
		{"exit 0 writes", []string{orders, "--html=" + filepath.Join(dir, "ok.html")}, 0, true, "pathkit report: wrote "},
		{"exit 2 still writes", []string{orders, "--fail-under", "90", "--html=" + filepath.Join(dir, "low.html")}, 2, true, "is below --fail-under 90%"},
		{"exit 1 writes nothing", []string{"../../testdata/fixtures/rules/...", "--html=" + filepath.Join(dir, "none.html")}, 1, false, "in scope but not analyzable"},
		{"a space instead of =", []string{"--html", "out.html"}, 1, false, "write --html=out.html (with =)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := runBinary(t, bin, append([]string{"report", "--traces", traces}, tt.args...)...)
			page := ""
			for _, a := range tt.args {
				if p, ok := strings.CutPrefix(a, "--html="); ok {
					page = p
				}
			}
			_, err := os.Stat(page)
			if code != tt.code || (err == nil) != tt.written || !strings.Contains(stderr, tt.stderr) {
				t.Errorf("exit %d, written=%v, stderr %q; want exit %d, written=%v, %q", code, err == nil, stderr, tt.code, tt.written, tt.stderr)
			}
			if tt.written {
				if _, err := os.Stat(filepath.Join(dir, "report-history.json")); err != nil {
					t.Errorf("no report-history.json next to the page")
				}
			}
		})
	}
}

// scripts/project-baseline.sh runs PathKit on a copy of a project and
// leaves the project alone. Run for real on the pilot, with the binary named
// by a RELATIVE path (the script changes folder, so a relative path must still work).
func TestBaselineScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary and runs the pilot's tests; skipped with -short (CI runs it)")
	}
	bash, err := exec.LookPath("bash")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs bash (not Windows)")
	}
	bin := buildPathkit(t)
	script, _ := filepath.Abs("../../scripts/project-baseline.sh")
	pilot, _ := filepath.Abs("../../testdata/pilot")
	out := filepath.Join(t.TempDir(), "out")
	// When this test fails, show what the script itself recorded, so one CI
	// run is enough to see why (the files are small).
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		files, _ := filepath.Glob(filepath.Join(out, "*.err"))
		files = append(files, filepath.Join(out, "exit-codes.txt"), filepath.Join(out, "test-output.txt"))
		for _, f := range files {
			if data, err := os.ReadFile(f); err == nil {
				t.Logf("%s:\n%.2000s", filepath.Base(f), data)
			}
		}
	})
	rel, err := filepath.Rel(mustGetwd(t), bin)
	if err != nil {
		t.Skip("no relative path to the binary on this system")
	}

	cmd := exec.Command(bash, script, pilot, out)
	cmd.Env = append(os.Environ(), "PATHKIT="+rel)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, o)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := read("report.txt"); !strings.Contains(got, "38 paths total · 20 covered · 18 missed · 52.6% project coverage") {
		t.Errorf("report.txt:\n%s", got)
	}
	if got := read("versions.txt"); !strings.Contains(got, "project folder unchanged: yes") || !strings.Contains(got, "v0.0.0-test") {
		t.Errorf("versions.txt:\n%s", got)
	}
	for _, name := range []string{"report.json", "coverage.json", "analyze.txt", "test-output.txt", "exit-codes.txt"} {
		if read(name) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	traces, _ := filepath.Glob(filepath.Join(out, "traces", "*.trace.json"))
	if len(traces) != 21 {
		t.Errorf("%d trace files, want 21", len(traces))
	}
	// A fake pathkit that fails everything: the script must not say it worked.
	fake := filepath.Join(t.TempDir(), "pathkit")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bash, script, pilot, filepath.Join(t.TempDir(), "out2"))
	cmd.Env = append(os.Environ(), "PATHKIT="+fake)
	if o, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(o), "every PathKit command failed") {
		t.Errorf("a script whose every command failed should exit non-zero: err=%v\n%s", err, o)
	}

	// An output folder inside the project is refused.
	cmd = exec.Command(bash, script, pilot, filepath.Join(pilot, "inside"))
	cmd.Env = append(os.Environ(), "PATHKIT="+bin)
	if o, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(o), "the output folder must be outside the project") {
		t.Errorf("err=%v\n%s", err, o)
	}
	if _, err := os.Stat(filepath.Join(pilot, "inside")); err == nil {
		t.Error("the refused run created a folder inside the project")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// copyTree copies a folder (not the generated .pathkit folders).
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() {
			if d.Name() == ".pathkit" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A project folder reached through a symbolic link (macOS temp folders are
// one: /var is a link to /private/var) must record the same traces as the
// real folder, with the relative target "./...". The Go tool reports files
// by their real path, so "go test" must run with the real working folder too.
func TestTestFromSymlinkedFolder(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary and runs the sample project's tests; skipped with -short (CI runs it)")
	}
	if runtime.GOOS == "windows" {
		t.Skip("folder links need extra rights on Windows, and the fix does not apply there")
	}
	bin := buildPathkit(t)
	pilot, _ := filepath.Abs("../../testdata/pilot")
	base := t.TempDir()
	real := filepath.Join(base, "real")
	copyTree(t, pilot, real)
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot make a symbolic link here: %v", err)
	}
	for name, dir := range map[string]string{"real folder": real, "through a link": link} {
		cmd := exec.Command(bin, "test", "./...") // relative target, as people type it
		cmd.Dir = dir                             // os/exec sets PWD to this (logical) path
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if !strings.Contains(string(out), "recorded 21 traces (21 complete)") {
			t.Errorf("%s: want 21 traces recorded:\n%s", name, out)
		}
	}
}
