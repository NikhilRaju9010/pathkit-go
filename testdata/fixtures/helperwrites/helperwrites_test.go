package helperwrites

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(wf)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

func TestViaPointer(t *testing.T)  { run(t, ViaPointer) }
func TestViaReceiver(t *testing.T) { run(t, ViaReceiver) }
func TestViaMap(t *testing.T)      { run(t, ViaMap) }
func TestViaSlice(t *testing.T)    { run(t, ViaSlice) }
func TestViaCapture(t *testing.T)  { run(t, ViaCapture) }
func TestNearMiss(t *testing.T)    { run(t, NearMiss) }
