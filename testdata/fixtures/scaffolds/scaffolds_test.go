package scaffolds

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Handle)
	env.RegisterActivity(Audit)
	env.RegisterActivity(Notify)
	env.RegisterActivity(Assess)
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
}

// One plain run of each workflow: the scenarios that need other input, another
// result or a signal are what a scaffold is for.
func TestRoutePlain(t *testing.T)  { run(t, Route, Request{Type: "A", Items: []string{"x"}}) }
func TestReviewPlain(t *testing.T) { run(t, Review, Request{}) }
func TestKindsNone(t *testing.T)   { run(t, Kinds, Request{}) }

func TestOpaquePlain(t *testing.T) { run(t, Opaque, Request{}) }
