package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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
		t.Skip("builds the binary; skipped with -short")
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
