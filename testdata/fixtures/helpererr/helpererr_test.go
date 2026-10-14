package helpererr

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// The helper fails, so the run returns early: under "pathkit test" its trace
// (steps J1.false) fits no path.
func TestEmptyInputFails(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Flow, "")
	if env.GetWorkflowError() == nil {
		t.Fatal("expected an error")
	}
}
