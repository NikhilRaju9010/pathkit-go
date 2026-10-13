package closedchannelleak

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(wf)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}

// Run for real by TestLiveFixtures: each takes "if !more" (J1.true).
func TestSameNameClosedReachesTheClosedSide(t *testing.T) { run(t, SameNameClosed) }
func TestInlineCloseReachesTheClosedSide(t *testing.T)    { run(t, InlineClose) }
func TestHelperClosedReachesTheClosedSide(t *testing.T)   { run(t, HelperClosed) }
