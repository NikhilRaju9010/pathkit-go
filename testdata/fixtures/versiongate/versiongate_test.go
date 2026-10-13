package versiongate

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// The old side is reachable with the SDK's own mock: proof that the
// detector's label is needs-special-test, not cannot-be-tested.
func TestOldBranchWithOnGetVersion(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnGetVersion("change-a", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.ExecuteWorkflow(CatchEqDefault)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatal(env.GetWorkflowError())
	}
}

// Without the mock, the newest version is returned and the new side runs.
func TestNewBranchByDefault(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(CatchEqDefault)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatal(env.GetWorkflowError())
	}
}
