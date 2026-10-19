package scenariotraps

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestVariable(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Charge)
	env.RegisterActivity(Refund)
	env.ExecuteWorkflow(Variable, Input{})
	if !env.IsWorkflowCompleted() {
		t.Fatal("did not complete")
	}
}
