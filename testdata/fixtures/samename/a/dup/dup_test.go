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

func TestPositive(t *testing.T) { run(t, 1) }
