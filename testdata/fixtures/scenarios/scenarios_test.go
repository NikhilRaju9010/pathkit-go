package scenarios

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, args ...any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	for _, a := range []any{Charge, ChargeBig, A, B, C, D, E} {
		env.RegisterActivity(a)
	}
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
}

func TestLogic(t *testing.T)        { run(t, Logic, Input{}); run(t, Logic, Input{Big: true}) }
func TestDifferent(t *testing.T)    { run(t, Different, Input{}) }
func TestOrderMatters(t *testing.T) { run(t, OrderMatters, Input{Big: true}) }
func TestWide(t *testing.T)         { run(t, Wide, Input{Count: 3}) }
func TestMany(t *testing.T)         { run(t, Many, Input{}) }
func TestSleeper(t *testing.T)      { run(t, Sleeper, Input{Big: true}) }

// TestLogicAgain runs what TestLogic already runs: it adds nothing new.
func TestLogicAgain(t *testing.T) { run(t, Logic, Input{}) }
