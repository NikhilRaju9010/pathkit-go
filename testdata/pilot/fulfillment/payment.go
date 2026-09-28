package fulfillment

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// PaymentWorkflow is the child workflow started by
// OrderFulfillmentWorkflow. Big orders get an extra fraud review.
func PaymentWorkflow(ctx workflow.Context, o Order) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	var authID string
	err := workflow.ExecuteActivity(ctx, AuthorizeCard, o).Get(ctx, &authID)
	if err != nil {
		return "", err
	}

	if o.AmountCents > 100_000 {
		err = workflow.ExecuteActivity(ctx, FraudReview, o).Get(ctx, nil)
		if err != nil {
			return "", err
		}
	}
	return authID, nil
}
