package approval

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestApprovalApproved(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()

	var statusBeforeSignal string
	env.RegisterDelayedCallback(func() {
		// The query answers while the workflow is still running.
		v, err := env.QueryWorkflow("status")
		require.NoError(t, err)
		require.NoError(t, v.Get(&statusBeforeSignal))

		env.SignalWorkflow("decision", "approved")
	}, time.Hour)

	env.ExecuteWorkflow(ApprovalWorkflow, Request{ID: "req-1", Requester: "sam"})

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, "waiting", statusBeforeSignal)
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "approved", result)
}

func TestApprovalTimesOut(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()

	start := time.Now()
	env.ExecuteWorkflow(ApprovalWorkflow, Request{ID: "req-2", Requester: "sam"})

	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "expired", result)
	// The 48h wait is skipped by the test environment's fake clock.
	require.Less(t, time.Since(start), 10*time.Second)
}
