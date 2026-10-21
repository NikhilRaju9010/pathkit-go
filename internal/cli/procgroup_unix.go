//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// stopWithChildren puts the command in its own process group and, when its
// context ends, stops the whole group: go test, the test program it started
// and anything that program started. Without it the test program of a run that
// timed out keeps running (and keeps its memory).
func stopWithChildren(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
