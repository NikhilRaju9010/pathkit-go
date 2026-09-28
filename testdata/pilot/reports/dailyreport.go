// Package reports holds a workflow run every morning by a Temporal Schedule
// (cron spec "0 6 * * *", created in cmd/worker).
package reports

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Summary struct {
	GeneratedAt time.Time
	Rows        int
}

// DailyReportWorkflow builds a report of everything since the previous
// run (or the last 24 hours on the first run) and emails it.
func DailyReportWorkflow(ctx workflow.Context) (Summary, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Minute})

	since := workflow.Now(ctx).Add(-24 * time.Hour)
	if workflow.HasLastCompletionResult(ctx) {
		var last Summary
		// Not a Temporal call in PathKit's list: transparent (D2), and its
		// "no error" side is the true side.
		if err := workflow.GetLastCompletionResult(ctx, &last); err == nil {
			since = last.GeneratedAt
		}
	}

	var sum Summary
	err := workflow.ExecuteActivity(ctx, BuildReport, since).Get(ctx, &sum)
	if err != nil {
		return Summary{}, err
	}
	err = workflow.ExecuteActivity(ctx, EmailReport, sum).Get(ctx, nil)
	if err != nil {
		return Summary{}, err
	}
	return sum, nil
}
