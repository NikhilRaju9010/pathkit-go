// Package shipment holds a workflow that races a delivery-update signal
// against a 72-hour timer with a workflow.Selector.
package shipment

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Shipment struct {
	OrderID string
	Address string
}

type DeliveryUpdate struct {
	Status string // "delivered", "returned", or anything else for an exception
}

// ShipmentWorkflow creates a label, then waits for the carrier's delivery
// update or gives the parcel up as lost after 72 hours, and tells the
// customer either way.
func ShipmentWorkflow(ctx workflow.Context, in Shipment) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	var label string
	err := workflow.ExecuteActivity(ctx, CreateLabel, in).Get(ctx, &label)
	if err != nil {
		return "", err
	}

	var outcome string
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timer := workflow.NewTimer(timerCtx, 72*time.Hour)

	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "delivery-update"), func(c workflow.ReceiveChannel, more bool) {
		var u DeliveryUpdate
		c.Receive(ctx, &u)
		cancelTimer()
		switch u.Status {
		case "delivered":
			outcome = "delivered"
		case "returned":
			outcome = "returned"
		default:
			outcome = "exception"
		}
	})
	sel.AddFuture(timer, func(f workflow.Future) {
		outcome = "lost"
	})
	sel.Select(ctx)

	err = workflow.ExecuteActivity(ctx, NotifyCustomer, in.OrderID, outcome).Get(ctx, nil)
	if err != nil {
		return "", err
	}
	return outcome, nil
}
