package update_named

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestUpdateRefused(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("add", "u1", &testsuite.TestUpdateCallback{
			OnAccept: func() {}, OnReject: func(error) {}, OnComplete: func(any, error) {}}, -1)
		env.SignalWorkflow("stop", nil)
	}, 0)
	env.ExecuteWorkflow(Run)
	if !env.IsWorkflowCompleted() {
		t.Fatal("not completed")
	}
}
