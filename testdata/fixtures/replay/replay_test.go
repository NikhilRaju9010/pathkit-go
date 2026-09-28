package replay

import (
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

func TestTestEnvironmentNeverReplays(t *testing.T) {
	for run := 0; run < 3; run++ {
		Starts.Store(0)
		ReplayedSteps.Store(0)

		var s testsuite.WorkflowTestSuite
		env := s.NewTestWorkflowEnvironment()
		env.RegisterActivity(Work)
		env.RegisterDelayedCallback(func() { env.SignalWorkflow("go", "now") }, 2*time.Hour)
		env.ExecuteWorkflow(ReplayProbeWorkflow)

		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("workflow failed: %v", err)
		}
		var result string
		if err := env.GetWorkflowResult(&result); err != nil || result != "done now" {
			t.Fatalf("result = %q, %v", result, err)
		}
		if got := Starts.Load(); got != 1 {
			t.Errorf("run %d: workflow function started %d times, want exactly 1 (no replay)", run, got)
		}
		if got := ReplayedSteps.Load(); got != 0 {
			t.Errorf("run %d: IsReplaying was true at %d steps, want 0", run, got)
		}
	}
}
