package fulfillment

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestPaymentSmallOrder(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	small := Order{ID: "o1", AmountCents: 2500}
	env.OnActivity(AuthorizeCard, mock.Anything, small).Return("auth-o1", nil)

	env.ExecuteWorkflow(PaymentWorkflow, small)

	require.NoError(t, env.GetWorkflowError())
	env.AssertActivityNotCalled(t, "FraudReview", mock.Anything, mock.Anything)
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "auth-o1", result)
}

func TestPaymentBigOrderPassesReview(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	big := Order{ID: "o2", AmountCents: 250_000}
	env.OnActivity(AuthorizeCard, mock.Anything, big).Return("auth-o2", nil)
	env.OnActivity(FraudReview, mock.Anything, big).Return(nil)

	env.ExecuteWorkflow(PaymentWorkflow, big)

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}
