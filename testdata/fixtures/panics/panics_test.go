package panics

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// panicLine finds the line in panics.go that holds the panic.
func panicLine(t *testing.T) int {
	t.Helper()
	src, err := os.ReadFile("panics.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "return explode(x), nil") {
			return i + 1
		}
	}
	t.Fatal("panic line not found")
	return 0
}

func TestPanicPointsAtRealLine(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(PanicWorkflow, 1)

	var perr *temporal.PanicError
	if !errors.As(env.GetWorkflowError(), &perr) {
		t.Fatalf("want a panic error, got %v", env.GetWorkflowError())
	}
	want := fmt.Sprintf("panics/panics.go:%d", panicLine(t))
	if !strings.Contains(perr.StackTrace(), want) {
		t.Errorf("stack trace does not name %s:\n%s", want, perr.StackTrace())
	}
	if strings.Contains(perr.StackTrace(), ".pathkit") {
		t.Errorf("stack trace names pathkit's overlay copy instead of the real file:\n%s", perr.StackTrace())
	}
}
