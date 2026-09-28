// Command worker runs every pilot workflow and activity against a Temporal
// server, and makes sure the daily-report Schedule exists. It needs a real
// server, so the pilot's tests only compile it; they never run it.
package main

import (
	"context"
	"log"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"example.com/pilot/approval"
	"example.com/pilot/billing"
	"example.com/pilot/fulfillment"
	"example.com/pilot/orders"
	"example.com/pilot/polling"
	"example.com/pilot/reports"
	"example.com/pilot/shipment"
)

const taskQueue = "pilot"

func main() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("unable to connect to Temporal:", err)
	}
	defer c.Close()

	if err := ensureDailyReportSchedule(context.Background(), c); err != nil {
		log.Println("daily-report schedule not created (it may already exist):", err)
	}

	w := worker.New(c, taskQueue, worker.Options{})

	w.RegisterWorkflow(orders.OrderWorkflow)
	w.RegisterWorkflow(approval.ApprovalWorkflow)
	w.RegisterWorkflow(polling.ReportPollingWorkflow)
	w.RegisterWorkflow(shipment.ShipmentWorkflow)
	w.RegisterWorkflow(fulfillment.OrderFulfillmentWorkflow)
	w.RegisterWorkflow(fulfillment.PaymentWorkflow)
	w.RegisterWorkflow(billing.SubscriptionWorkflow)
	w.RegisterWorkflow(reports.DailyReportWorkflow)

	w.RegisterActivity(orders.ChargeCard)
	w.RegisterActivity(polling.CheckStatus)
	w.RegisterActivity(shipment.CreateLabel)
	w.RegisterActivity(shipment.NotifyCustomer)
	w.RegisterActivity(fulfillment.ReserveInventory)
	w.RegisterActivity(fulfillment.ReleaseInventory)
	w.RegisterActivity(fulfillment.ShipOrder)
	w.RegisterActivity(fulfillment.AuthorizeCard)
	w.RegisterActivity(fulfillment.FraudReview)
	w.RegisterActivity(billing.ChargeMonthly)
	w.RegisterActivity(reports.BuildReport)
	w.RegisterActivity(reports.EmailReport)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("worker stopped:", err)
	}
}

// ensureDailyReportSchedule runs DailyReportWorkflow every day at 06:00.
func ensureDailyReportSchedule(ctx context.Context, c client.Client) error {
	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: "daily-report",
		Spec: client.ScheduleSpec{
			CronExpressions: []string{"0 6 * * *"},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        "daily-report",
			Workflow:  reports.DailyReportWorkflow,
			TaskQueue: taskQueue,
		},
	})
	return err
}
