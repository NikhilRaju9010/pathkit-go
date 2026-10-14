package builtlist

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

// Near-misses, run for real: in each of these the first loop is skipped or
// has run while the second loop did the opposite, so the path "one loop
// skipped, the other ran" is reachable and no detector may call it
// impossible.
func TestConditionalAppendAllSkipped(t *testing.T) {
	run(t, ConditionalAppend, In{Items: []string{""}})
}
func TestContinueBeforeAppendAllSkipped(t *testing.T) {
	run(t, ContinueBeforeAppend, In{Items: []string{""}})
}
func TestStartsNotEmptyFirstLoopSkipped(t *testing.T)  { run(t, StartsNotEmpty, In{}) }
func TestAppendedBetweenFirstLoopSkipped(t *testing.T) { run(t, AppendedBetween, In{}) }
func TestReplacedBetweenFirstLoopRan(t *testing.T) {
	run(t, ReplacedBetween, In{Items: []string{"a"}, Seed: []string{"x"}})
}
func TestClosureAppendsFirstLoopSkipped(t *testing.T) {
	run(t, ClosureAppends, In{Seed: []string{"x"}})
}
func TestReturnBeforeAppendFirstLoopRan(t *testing.T) {
	run(t, ReturnBeforeAppend, In{Items: []string{"stop"}})
}

// Found by the independent review: the list is cleared through a pointer
// that was not written with &, so "first loop ran, second skipped" is
// reachable.
func TestMethodResetFirstLoopRanSecondSkipped(t *testing.T) {
	run(t, MethodReset, In{Items: []string{"a"}, Reset: true})
}
func TestMethodValueResetFirstLoopRanSecondSkipped(t *testing.T) {
	run(t, MethodValueReset, In{Items: []string{"a"}, Reset: true})
}
func TestParenAddressFirstLoopRanSecondSkipped(t *testing.T) {
	run(t, ParenAddress, In{Items: []string{"a"}, Reset: true})
}
