package reports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var built = Summary{GeneratedAt: time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC), Rows: 12}

func TestDailyReportFirstRun(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(BuildReport, mock.Anything, mock.Anything).Return(built, nil)
	env.OnActivity(EmailReport, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(DailyReportWorkflow)

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestDailyReportLaterRunStartsFromLastReport(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	previous := Summary{GeneratedAt: time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC), Rows: 5}
	// Pretend the schedule already ran once before.
	env.SetLastCompletionResult(previous)
	fromPrevious := mock.MatchedBy(func(since time.Time) bool { return since.Equal(previous.GeneratedAt) })
	env.OnActivity(BuildReport, mock.Anything, fromPrevious).Return(built, nil)
	env.OnActivity(EmailReport, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(DailyReportWorkflow)

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestDailyReportBuildFails(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(BuildReport, mock.Anything, mock.Anything).
		Return(Summary{}, temporal.NewNonRetryableApplicationError("database down", "DBDown", nil))

	env.ExecuteWorkflow(DailyReportWorkflow)

	require.ErrorContains(t, env.GetWorkflowError(), "database down")
}
