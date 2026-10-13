package unnamedctx

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func sign(t *testing.T, n int) string {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Sign, n)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSignPositive(t *testing.T) {
	if got := sign(t, 5); got != "positive" {
		t.Fatalf("got %q", got)
	}
}

func TestSignNegative(t *testing.T) {
	if got := sign(t, -5); got != "negative" {
		t.Fatalf("got %q", got)
	}
}

func TestSignZero(t *testing.T) {
	if got := sign(t, 0); got != "zero" {
		t.Fatalf("got %q", got)
	}
}

func TestStrictEmptyName(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Strict, "", 1)
	if env.GetWorkflowError() == nil {
		t.Fatal("want an error")
	}
}

func TestStrictNamed(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(Strict, "x", 1)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}
