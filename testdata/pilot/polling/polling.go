// Package polling holds a workflow that polls a report job in a retry loop,
// giving up after a fixed number of polls.
package polling

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type ReportRequest struct {
	ReportID string
	MaxPolls int
}

// ReportPollingWorkflow checks the report job's status up to MaxPolls
// times, ten seconds apart.
func ReportPollingWorkflow(ctx workflow.Context, in ReportRequest) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	for attempt := 1; attempt <= in.MaxPolls; attempt++ {
		var status string
		err := workflow.ExecuteActivity(ctx, CheckStatus, in.ReportID).Get(ctx, &status)
		if err != nil {
			return "", err
		}

		switch status {
		case "complete":
			return "done", nil
		case "failed":
			return "failed", nil
		}

		_ = workflow.Sleep(ctx, 10*time.Second)
	}
	return "timed out", nil
}
