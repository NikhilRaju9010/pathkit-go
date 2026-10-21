package conditions

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Ship)
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
}

// Chain: the first part is false once, the rest are reached on the second run.
func TestChainZeroAmount(t *testing.T) { run(t, Chain, Request{Amount: 0}) }
func TestChainKindA(t *testing.T) {
	run(t, Chain, Request{Amount: 5, Kind: "A", Region: "north"})
}
func TestChainExpressNoRegion(t *testing.T) {
	run(t, Chain, Request{Amount: 5, Kind: "B", Express: true})
}

func TestSingle(t *testing.T)   { run(t, Single, Request{Express: true}) }
func TestRepeated(t *testing.T) { run(t, Repeated, true) }

func TestHelperAnd(t *testing.T) { run(t, HelperAnd, 5, 20) }

func TestLooped(t *testing.T) { run(t, Looped, 2, Flag(false)) }
func TestNamed(t *testing.T)  { run(t, Named, Flag(true), 5) }
