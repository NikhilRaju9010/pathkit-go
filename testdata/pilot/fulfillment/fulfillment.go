// Package fulfillment holds a saga: reserve stock, take payment in a child
// workflow, ship, and release the stock again if a later step fails.
package fulfillment

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Order struct {
	ID          string
	SKU         string
	AmountCents int64
}

// OrderFulfillmentWorkflow runs the order saga. The deferred function is
// the compensation: it releases the reserved stock when the workflow ends
// with an error.
func OrderFulfillmentWorkflow(ctx workflow.Context, o Order) (result string, err error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	err = workflow.ExecuteActivity(ctx, ReserveInventory, o).Get(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			dctx, _ := workflow.NewDisconnectedContext(ctx)
			_ = workflow.ExecuteActivity(dctx, ReleaseInventory, o).Get(dctx, nil)
		}
	}()

	var paymentID string
	err = workflow.ExecuteChildWorkflow(newChildCtx(ctx), PaymentWorkflow, o).Get(ctx, &paymentID)
	if err != nil {
		return "", err
	}

	err = workflow.ExecuteActivity(ctx, ShipOrder, o, paymentID).Get(ctx, nil)
	if err != nil {
		return "", err
	}
	AuditLog(ctx, "order fulfilled: "+o.ID)
	return "fulfilled", nil
}
