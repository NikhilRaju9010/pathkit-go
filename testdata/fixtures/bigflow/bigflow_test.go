package bigflow

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

func bits(on ...int) [24]bool {
	var in [24]bool
	for _, i := range on {
		in[i] = true
	}
	return in
}

func TestAllOff(t *testing.T) { run(t, TwentyFourIfs, bits()) }
func TestAllOn(t *testing.T) {
	var on []int
	for i := 0; i < 24; i++ {
		on = append(on, i)
	}
	run(t, TwentyFourIfs, bits(on...))
}
func TestFirstStageOnly(t *testing.T) { run(t, TwentyFourIfs, bits(0, 1, 2, 3, 4, 5, 6, 7)) }
func TestLastStageOnly(t *testing.T)  { run(t, TwentyFourIfs, bits(16, 17, 18, 19, 20, 21, 22, 23)) }
func TestScattered(t *testing.T)      { run(t, TwentyFourIfs, bits(1, 9, 17)) }
func TestEarlyA(t *testing.T)         { run(t, EarlyExits, true, false, false, [20]bool{}) }
func TestEarlyB(t *testing.T)         { run(t, EarlyExits, false, true, false, [20]bool{}) }
func TestEarlyNone(t *testing.T)      { run(t, EarlyExits, false, false, false, [20]bool{}) }
func TestEarlyThroughAll(t *testing.T) {
	run(t, EarlyExits, false, false, false, [20]bool{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true})
}
