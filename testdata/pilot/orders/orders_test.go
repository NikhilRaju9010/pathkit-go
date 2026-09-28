package orders

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var validOrder = Order{ID: "o1", SKU: " abc-1 ", AmountCents: 2500}

func TestOrderRejected(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(OrderWorkflow, Order{ID: "o1", SKU: "ABC-1", AmountCents: 0})

	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "rejected", result)
}

func TestOrderChargeFails(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(ChargeCard, mock.Anything, mock.Anything).
		Return(Receipt{}, temporal.NewNonRetryableApplicationError("card declined", "CardDeclined", nil))

	env.ExecuteWorkflow(OrderWorkflow, validOrder)

	require.ErrorContains(t, env.GetWorkflowError(), "card declined")
}

func TestOrderCharged(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(ChargeCard, mock.Anything, Order{ID: "o1", SKU: "ABC-1", AmountCents: 2500}).
		Return(Receipt{ID: "r-42"}, nil)

	env.ExecuteWorkflow(OrderWorkflow, validOrder)

	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "r-42", result)
}
