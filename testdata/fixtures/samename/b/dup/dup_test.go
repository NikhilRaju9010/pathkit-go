package dup

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, n int) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Work, n)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

// Only the negative path is tested here; the positive path is tested only
// in a/dup, which has the same first decision and so the same stable id.
func TestNegative(t *testing.T) { run(t, -1) }
