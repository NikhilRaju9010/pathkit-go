package loopfirst

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}

// Near-misses, run for real: each reaches "left the loop before its first
// trip", so no detector may call that path impossible.
func TestSetBeforeSkipsTheLoop(t *testing.T)         { run(t, SetBefore, In{Flag: true}) }
func TestClosureSetsBeforeSkipsTheLoop(t *testing.T) { run(t, ClosureSetsBefore, In{Flag: true}) }
func TestAddressBeforeSkipsTheLoop(t *testing.T)     { run(t, AddressBefore, In{Flag: true}) }
func TestFromParameterSkipsTheLoop(t *testing.T)     { run(t, FromParameter, true) }
func TestDeclaredOutsideOuterLoopSecondTrip(t *testing.T) {
	run(t, DeclaredOutsideOuterLoop, In{Items: []string{"a", "b"}})
}
func TestAssignedBeforeSkipsTheLoop(t *testing.T) { run(t, AssignedBefore, In{Flag: true}) }
func TestPackageLevelSecondRun(t *testing.T) {
	stopped = true
	defer func() { stopped = false }()
	run(t, PackageLevel, In{})
}

// Found by the independent review.
func TestParenAssignSkipsTheLoop(t *testing.T) { run(t, ParenAssign, In{Flag: true}) }
