package historysuggest

import (
	"errors"
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// The suggestion branch is reachable with the SDK's own setter: proof that
// the detector's label is needs-special-test, not cannot-be-tested.
func TestSuggestedWithSetter(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.SetContinueAsNewSuggested(true)
	env.ExecuteWorkflow(CatchDirect)
	var can *workflow.ContinueAsNewError
	if !errors.As(env.GetWorkflowError(), &can) {
		t.Fatalf("want continue-as-new, got %v", env.GetWorkflowError())
	}
}

func TestNotSuggestedByDefault(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(CatchDirect)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatal(env.GetWorkflowError())
	}
}
