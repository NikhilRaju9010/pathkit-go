package zeropaths

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// Spin is not run: it never ends.
func TestPlain(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Plain, true)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatal("Plain should complete without an error")
	}
}
