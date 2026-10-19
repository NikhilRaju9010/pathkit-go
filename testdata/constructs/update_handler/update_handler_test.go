package update_handler

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestUpdate(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(ctx context.Context, n int) error { return nil }, activity.RegisterOptions{Name: "Record"})
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("add", "u1", &testsuite.TestUpdateCallback{
			OnAccept: func() {}, OnReject: func(error) {}, OnComplete: func(any, error) {}}, 2)
		env.UpdateWorkflow("add", "u2", &testsuite.TestUpdateCallback{
			OnAccept: func() {}, OnReject: func(error) {}, OnComplete: func(any, error) {}}, -1)
		env.SignalWorkflow("stop", nil)
	}, 0)
	env.ExecuteWorkflow(Run)
	if !env.IsWorkflowCompleted() {
		t.Fatal("not completed")
	}
}
