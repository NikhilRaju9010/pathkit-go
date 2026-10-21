//go:build unix

package cli

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A command that started a child must not leave the child running when its
// context ends (a forced test program that loops for ever).
func TestStopWithChildrenStopsTheChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", "sleep 60 & echo $!; wait")
	stopWithChildren(c)
	out, _ := c.Output()
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("no child pid in %q", out)
	}
	for i := 0; i < 50; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			return // gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("the child %d is still running", pid)
}
