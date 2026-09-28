package cli

import (
	"errors"
	"fmt"
	"strings"
)

// Exit codes. main.go is the only place that turns these into a real
// process exit; everything else just returns them.
const (
	// ExitOK means the command did what was asked.
	ExitOK = 0
	// ExitError means a real error: bad arguments, missing file, and so on.
	ExitError = 1
	// ExitBelowThreshold means the report was produced but coverage is
	// below --fail-under. A separate code lets CI tell "PathKit broke"
	// apart from "coverage is too low".
	ExitBelowThreshold = 2
)

// exitError is an error that knows which exit code it stands for.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// userError is a real error (exit code 1), e.g. a missing argument.
func userError(format string, args ...any) error {
	return &exitError{code: ExitError, msg: fmt.Sprintf(format, args...)}
}

// belowThresholdError is returned when coverage is under --fail-under
// (exit code 2).
func belowThresholdError(format string, args ...any) error {
	return &exitError{code: ExitBelowThreshold, msg: fmt.Sprintf(format, args...)}
}

// exitCodeFor returns the exit code an error stands for. Errors that don't
// carry a code (for example cobra's own "unknown flag" errors) are real
// errors, code 1.
func exitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return ExitError
}

// formatError builds the one error line every PathKit command prints:
// "pathkit <command>: <message>", or "pathkit: <message>" when no
// subcommand is involved.
func formatError(command string, err error) string {
	msg := cleanMessage(err.Error())
	if command == "" {
		return "pathkit: " + msg + "\n"
	}
	return "pathkit " + command + ": " + msg + "\n"
}

// cleanMessage keeps PathKit errors to one line. Cobra's "unknown command"
// error adds ` for "pathkit"` and a multi-line "Did you mean" block; only
// the first line is kept, without the redundant suffix.
func cleanMessage(msg string) string {
	msg, _, _ = strings.Cut(msg, "\n")
	msg = strings.TrimSuffix(msg, ` for "pathkit"`)
	return strings.TrimSpace(msg)
}
