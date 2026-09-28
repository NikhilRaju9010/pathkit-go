package scope

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// lowerFlow runs for real, so PathKit can record a workflow that only
// "include" added.
func TestLowerFlowPositive(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(lowerFlow, 5)
	var got string
	if err := env.GetWorkflowResult(&got); err != nil || got != "positive" {
		t.Fatalf("got %q, %v; want positive", got, err)
	}
}
