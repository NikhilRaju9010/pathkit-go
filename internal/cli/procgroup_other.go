//go:build !unix

package cli

import "os/exec"

// stopWithChildren: on this system only the command itself is stopped.
func stopWithChildren(c *exec.Cmd) {}
