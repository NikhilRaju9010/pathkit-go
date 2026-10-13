package closedchannel

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// The near-miss that proves the rule: a channel the workflow makes and
// closes itself DOES reach the "more == false" side, so PathKit must never
// call that side impossible for such a channel. This test runs it for real.
func TestOwnClosedReachesTheClosedSide(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(OwnClosed)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

// Near-misses of the "declared first, assigned once" shape, run for real:
// the workflow's own channel is closed, so the closed side is reachable.
func TestLateOwnClosedReachesTheClosedSide(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(LateOwnClosed)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}

func TestLateAssignedTwiceReachesTheClosedSide(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(LateAssignedTwice)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}
