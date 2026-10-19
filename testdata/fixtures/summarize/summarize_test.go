package summarize

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

func TestChainAllFast(t *testing.T) {
	run(t, Chain, [12]bool{true, true, true, true, true, true, true, true, true, true, true, true})
}
func TestChainSomeSlow(t *testing.T) { run(t, Chain, [12]bool{false, true}) }
func TestSmall(t *testing.T)         { run(t, Small, true, false) }
func TestNested(t *testing.T)        { run(t, Nested, true) }
func TestLooping(t *testing.T)       { run(t, Looping, 2) }
