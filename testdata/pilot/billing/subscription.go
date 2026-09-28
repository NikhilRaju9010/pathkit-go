// Package billing holds a long-running subscription workflow that charges
// monthly and continues as a new run to keep its history short.
package billing

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Subscription struct {
	ID           string
	CyclesPerRun int
}

// SubscriptionWorkflow charges CyclesPerRun months, then continues as new.
func SubscriptionWorkflow(ctx workflow.Context, s Subscription) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	for i := 0; i < s.CyclesPerRun; i++ {
		err := workflow.ExecuteActivity(ctx, ChargeMonthly, s).Get(ctx, nil)
		if err != nil {
			return err
		}
		_ = workflow.Sleep(ctx, 30*24*time.Hour)
	}
	return workflow.NewContinueAsNewError(ctx, SubscriptionWorkflow, s)
}
