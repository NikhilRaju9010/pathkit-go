package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// runnerOptions are the safe-runner flags of "pathkit test".
type runnerOptions struct {
	noNetwork bool
	skipFile  string
}

// readSkipFile reads a skip list: one regular expression per line; blank
// lines and lines starting with # are ignored.
func readSkipFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, userError("could not read the skip file %s: %v", path, err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, userError("could not read the skip file %s: %v", path, err)
	}
	return out, nil
}

// apply adds the skip list to the go test arguments. A -skip the user passed
// after "--" is joined with it (either one skips a test).
func (o runnerOptions) apply(goArgs []string) ([]string, error) {
	if o.skipFile == "" {
		return goArgs, nil
	}
	list, err := readSkipFile(o.skipFile)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return goArgs, nil
	}
	parts := make([]string, len(list))
	for i, l := range list {
		parts[i] = "(" + l + ")"
	}
	pattern := strings.Join(parts, "|")
	out := append([]string(nil), goArgs...)
	for i, a := range out {
		for _, name := range []string{"-skip", "--skip", "-test.skip"} {
			switch {
			case a == name && i+1 < len(out):
				out[i+1] = "(" + out[i+1] + ")|" + pattern
				return out, nil
			case strings.HasPrefix(a, name+"="):
				out[i] = name + "=(" + strings.TrimPrefix(a, name+"=") + ")|" + pattern
				return out, nil
			}
		}
	}
	return append(out, "-skip", pattern), nil
}

// command is the program that runs go test and the arguments before "go":
// "go" alone, or "unshare -rn go" with --no-network. It refuses when the
// network cannot be switched off: no silent fallback.
func (o runnerOptions) command() (string, []string, error) {
	if !o.noNetwork {
		return "go", nil, nil
	}
	if runtime.GOOS != "linux" {
		return "", nil, userError("--no-network works only on Linux (it uses \"unshare -rn\"); on %s run the tests where the network is off, or leave the flag out", runtime.GOOS)
	}
	path, err := exec.LookPath("unshare")
	if err != nil {
		return "", nil, userError("--no-network needs the \"unshare\" program (part of util-linux), and it was not found; install it, or leave the flag out and switch the network off another way")
	}
	// try it: some systems forbid unprivileged user namespaces
	if out, err := exec.Command(path, "-rn", "true").CombinedOutput(); err != nil {
		return "", nil, userError("--no-network cannot start a network namespace here (unshare -rn failed: %s); PathKit does not run the tests with the network on after you asked for it off", strings.TrimSpace(fmt.Sprintf("%v %s", err, out)))
	}
	return path, []string{"-rn", "go"}, nil
}
