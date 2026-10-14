package dataflow

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

// Near-miss run for real: the value is assigned again between the tests, so
// "true, then !ok true" is a reachable path and must stay testable.
func TestReassignedBothTrue(t *testing.T) {
	run(t, Reassigned, In{Flag: true, Items: nil})
}

// Near-miss run for real: the list is appended to between the loops, so the
// first loop is skipped and the second runs: reachable.
func TestListAppendedSecondLoopRuns(t *testing.T) {
	run(t, ListAppended, In{Items: nil})
}

// Near-miss run for real: two different lists, one empty and one not.
func TestDifferentListsOneEmpty(t *testing.T) {
	run(t, DifferentLists, In{Items: nil, Other: []string{"a"}})
}

// Near-misses found by the independent review, run for real: each of these
// paths is reachable, so no detector may call it impossible.
func TestLongStringsOnlyFirstMatches(t *testing.T) {
	run(t, LongStrings, "ORDER_STATUS_PENDING_PAYMENT")
}

func TestMapLengthFilledThenNotEmpty(t *testing.T) {
	run(t, MapLength, map[string]int{})
}

func TestZeroThenStoreBothFalse(t *testing.T) {
	run(t, ZeroThenStore, Box{S: S2{A: true}})
}
