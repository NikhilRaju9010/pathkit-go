package shipment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var parcel = Shipment{OrderID: "o1", Address: "1 Main St"}

func TestShipmentDelivered(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(CreateLabel, mock.Anything, parcel).Return("label-o1", nil)
	env.OnActivity(NotifyCustomer, mock.Anything, "o1", "delivered").Return(nil)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("delivery-update", DeliveryUpdate{Status: "delivered"})
	}, 24*time.Hour)

	env.ExecuteWorkflow(ShipmentWorkflow, parcel)

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "delivered", result)
}

func TestShipmentTimerFiresLost(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(CreateLabel, mock.Anything, parcel).Return("label-o1", nil)
	env.OnActivity(NotifyCustomer, mock.Anything, "o1", "lost").Return(nil)

	start := time.Now()
	env.ExecuteWorkflow(ShipmentWorkflow, parcel)

	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "lost", result)
	// The 72h timer is skipped by the fake clock.
	require.Less(t, time.Since(start), 10*time.Second)
}

func TestShipmentLabelFails(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.OnActivity(CreateLabel, mock.Anything, parcel).
		Return("", temporal.NewNonRetryableApplicationError("bad address", "BadAddress", nil))

	env.ExecuteWorkflow(ShipmentWorkflow, parcel)

	require.ErrorContains(t, env.GetWorkflowError(), "bad address")
}
