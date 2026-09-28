package fulfillment

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var order = Order{ID: "o1", SKU: "ABC-1", AmountCents: 2500}

func TestFulfillmentPaymentFailsReleasesStock(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	env.OnActivity(ReserveInventory, mock.Anything, order).Return(nil)
	// The child workflow is mocked: its own code does not run.
	env.OnWorkflow(PaymentWorkflow, mock.Anything, order).
		Return("", temporal.NewNonRetryableApplicationError("card declined", "CardDeclined", nil))
	env.OnActivity(ReleaseInventory, mock.Anything, order).Return(nil)

	env.ExecuteWorkflow(OrderFulfillmentWorkflow, order)

	require.ErrorContains(t, env.GetWorkflowError(), "card declined")
	// The compensation in the defer ran.
	env.AssertActivityCalled(t, "ReleaseInventory", mock.Anything, order)
}

func TestFulfillmentSucceeds(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	env.OnActivity(ReserveInventory, mock.Anything, order).Return(nil)
	env.OnWorkflow(PaymentWorkflow, mock.Anything, order).Return("auth-o1", nil)
	env.OnActivity(ShipOrder, mock.Anything, order, "auth-o1").Return(nil)
	env.OnActivity(ReleaseInventory, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(OrderFulfillmentWorkflow, order)

	require.NoError(t, env.GetWorkflowError())
	env.AssertActivityNotCalled(t, "ReleaseInventory", mock.Anything, mock.Anything)
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "fulfilled", result)
}
