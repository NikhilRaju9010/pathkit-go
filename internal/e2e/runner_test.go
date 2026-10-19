package e2e

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A throw-away module with two tests: one reaches a server the test of this
// file listens on (so it passes with the network and fails without), one
// always fails (to be skipped by a skip file).
func networkModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/nn\n\ngo 1.26.0\n")
	write("nn_test.go", `package nn

import (
	"net"
	"os"
	"testing"
)

func TestDial(t *testing.T) {
	c, err := net.Dial("tcp", os.Getenv("PK_ADDR"))
	if err != nil {
		t.Fatalf("no network: %v", err)
	}
	c.Close()
}

func TestSkipMe(t *testing.T) { t.Fatal("always fails") }
func TestSkipMeToo(t *testing.T) { t.Fatal("always fails") }
`)
	return dir
}

func TestSafeRunner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen on the loopback")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	t.Setenv("PK_ADDR", ln.Addr().String())
	dir := networkModule(t)
	t.Chdir(dir)
	skips := filepath.Join(dir, "skips.txt")
	if err := os.WriteFile(skips, []byte("# tests that always fail\n\n^TestSkipMe$\n   ^TestSkipMeToo$  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the skip file: both failing tests are skipped, and only they
	if _, stderr, code := pathkit(t, "test", "./...", "--skip-file", skips); code != 0 {
		t.Errorf("with the skip file the run should pass: exit %d\n%s", code, stderr)
	}
	// near miss: without it the failing tests fail the run
	if _, _, code := pathkit(t, "test", "./..."); code == 0 {
		t.Error("without the skip file the always-failing tests should fail the run")
	}
	// a -skip after "--" is joined with the file's list, not replaced by it
	if _, stderr, code := pathkit(t, "test", "./...", "--skip-file", skips, "--", "-skip", "^TestDial$"); code != 0 {
		t.Errorf("both skip lists apply: exit %d\n%s", code, stderr)
	}
	if _, _, code := pathkit(t, "test", "./...", "--skip-file", filepath.Join(dir, "missing.txt")); code == 0 {
		t.Error("a missing skip file is an error")
	}

	// --no-network
	if runtime.GOOS != "linux" {
		_, stderr, code := pathkit(t, "test", "./...", "--no-network", "--skip-file", skips)
		if code == 0 || !strings.Contains(stderr, "works only on Linux") {
			t.Errorf("--no-network off Linux must refuse: exit %d %q", code, stderr)
		}
		return
	}
	if err := exec.Command("unshare", "-rn", "true").Run(); err != nil {
		_, stderr, code := pathkit(t, "test", "./...", "--no-network", "--skip-file", skips)
		if code == 0 || !strings.Contains(stderr, "--no-network") {
			t.Errorf("without unshare the flag must refuse, not fall back: exit %d %q", code, stderr)
		}
		return
	}
	out, stderr, code := pathkit(t, "test", "./...", "--no-network", "--skip-file", skips)
	if code == 0 || !strings.Contains(out, "no network") {
		t.Errorf("with --no-network the dialling test must fail: exit %d\n%s\n%s", code, out, stderr)
	}
	// and the same run with the network passes
	if _, stderr, code := pathkit(t, "test", "./...", "--skip-file", skips); code != 0 {
		t.Errorf("with the network the same run passes: exit %d\n%s", code, stderr)
	}
	// no silent fallback when unshare is missing
	t.Setenv("PATH", t.TempDir())
	if _, stderr, code := pathkit(t, "test", "./...", "--no-network"); code == 0 || !strings.Contains(stderr, "unshare") {
		t.Errorf("no unshare: exit %d %q", code, stderr)
	}
}
