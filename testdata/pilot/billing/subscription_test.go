package billing

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestSubscriptionContinuesAsNew(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	sub := Subscription{ID: "s1", CyclesPerRun: 2}
	env.OnActivity(ChargeMonthly, mock.Anything, sub).Return(nil).Times(2)

	env.ExecuteWorkflow(SubscriptionWorkflow, sub)

	var can *workflow.ContinueAsNewError
	require.True(t, errors.As(env.GetWorkflowError(), &can), "want a continue-as-new error, got %v", env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestSubscriptionChargeFails(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	sub := Subscription{ID: "s1", CyclesPerRun: 2}
	env.OnActivity(ChargeMonthly, mock.Anything, sub).
		Return(temporal.NewNonRetryableApplicationError("card expired", "CardExpired", nil))

	env.ExecuteWorkflow(SubscriptionWorkflow, sub)

	require.ErrorContains(t, env.GetWorkflowError(), "card expired")
}
