package returncall

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, failCharge bool, args ...any) error {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(ctx context.Context) error {
		if failCharge {
			return errors.New("declined")
		}
		return nil
	}, activity.RegisterOptions{Name: "Charge"})
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	return env.GetWorkflowError()
}

func TestReturnGetSuccess(t *testing.T) {
	if err := run(t, ReturnGet, false); err != nil {
		t.Fatal(err)
	}
}

func TestReturnGetFailure(t *testing.T) {
	if err := run(t, ReturnGet, true); err == nil {
		t.Fatal("want an error")
	}
}

func TestReturnSleep(t *testing.T) {
	if err := run(t, ReturnSleep, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestReturnWithValue(t *testing.T) {
	if err := run(t, ReturnWithValue, true); err == nil {
		t.Fatal("want an error")
	}
}

func TestReturnHelper(t *testing.T) {
	if err := run(t, ReturnHelper, false); err != nil {
		t.Fatal(err)
	}
}
