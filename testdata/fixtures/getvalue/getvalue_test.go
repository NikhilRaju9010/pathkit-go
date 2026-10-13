package getvalue

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	// A answers Ok=true, B answers Ok=false.
	env.RegisterActivityWithOptions(func(context.Context) (Res, error) { return Res{Ok: true}, nil }, activity.RegisterOptions{Name: "A"})
	env.RegisterActivityWithOptions(func(context.Context) (Res, error) { return Res{Ok: false}, nil }, activity.RegisterOptions{Name: "B"})
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}

// Near-misses, run for real: in each of these the first test sees
// Ok == true and the second test then sees Ok == false, so "true, then
// !Ok true" is reachable and no detector may call it impossible.
func TestSecondGetChangesIt(t *testing.T)           { run(t, SecondGet) }
func TestWriteBetweenChangesIt(t *testing.T)        { run(t, WriteBetween) }
func TestAddressPassedOnChangesIt(t *testing.T)     { run(t, AddressPassedOn) }
func TestMethodOnResultChangesIt(t *testing.T)      { run(t, MethodOnResult) }
func TestPointerAliasChangesIt(t *testing.T)        { run(t, PointerAlias) }
func TestPointerPassedChangesIt(t *testing.T)       { run(t, PointerPassed) }
func TestClosureWritesChangesIt(t *testing.T)       { run(t, ClosureWrites) }
func TestPointerToPointerChangesIt(t *testing.T)    { run(t, PointerToPointerPassed) }
func TestMethodValueOnResultChangesIt(t *testing.T) { run(t, MethodValueOnResult) }

// Found by the independent review: a parenthesised copy of the pointer.
func TestParenAliasChangesIt(t *testing.T) { run(t, ParenAlias) }
