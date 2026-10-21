package decisions

import (
	"errors"
	"github.com/stretchr/testify/mock"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, o Order, fail bool) (string, error) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	if fail {
		env.OnActivity(Notify, anyArgs()...).Return(errors.New("down"))
	} else {
		env.RegisterActivity(Notify)
	}
	env.ExecuteWorkflow(wf, o)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var out string
	err := env.GetWorkflowError()
	if err == nil {
		_ = env.GetWorkflowResult(&out)
	}
	return out, err
}

func anyArgs() []any { return []any{mock.Anything, mock.Anything} }

// Good tests check the result.
func TestCheckedSmall(t *testing.T) {
	if got, err := run(t, Checked, Order{Amount: 5}, false); err != nil || got != "small" {
		t.Fatalf("got %q, %v", got, err)
	}
}
func TestCheckedBig(t *testing.T) {
	if got, err := run(t, Checked, Order{Amount: 500}, false); err != nil || got != "big" {
		t.Fatalf("got %q, %v", got, err)
	}
}
func TestCheckedNoticeFails(t *testing.T) {
	if _, err := run(t, Checked, Order{Amount: 5}, true); err == nil {
		t.Fatal("want the failure back")
	}
}

// Weak tests run the workflow and look at nothing.
func TestUncheckedSmall(t *testing.T) { _, _ = run(t, Unchecked, Order{Amount: 5}, false) }
func TestUncheckedBig(t *testing.T)   { _, _ = run(t, Unchecked, Order{Amount: 500}, false) }
func TestUncheckedNoticeFails(t *testing.T) {
	_, _ = run(t, Unchecked, Order{Amount: 5}, true)
}
