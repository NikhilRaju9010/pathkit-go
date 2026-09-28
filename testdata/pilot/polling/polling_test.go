package polling

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func runPolling(t *testing.T, maxPolls int, statuses ...string) string {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	for _, st := range statuses {
		env.OnActivity(CheckStatus, mock.Anything, "r1").Return(st, nil).Once()
	}

	env.ExecuteWorkflow(ReportPollingWorkflow, ReportRequest{ReportID: "r1", MaxPolls: maxPolls})

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	return result
}

func TestPollingCompleteFirstTime(t *testing.T) {
	require.Equal(t, "done", runPolling(t, 5, "complete"))
}

func TestPollingJobFailed(t *testing.T) {
	require.Equal(t, "failed", runPolling(t, 5, "failed"))
}

func TestPollingPendingThenComplete(t *testing.T) {
	// Same path as "complete first time" once the loop is collapsed.
	require.Equal(t, "done", runPolling(t, 5, "pending", "complete"))
}

func TestPollingGivesUp(t *testing.T) {
	require.Equal(t, "timed out", runPolling(t, 2, "pending", "pending"))
}
