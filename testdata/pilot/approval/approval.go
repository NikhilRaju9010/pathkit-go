// Package approval holds a workflow that waits for a human decision
// (a signal), answers a status query, and gives up after a timeout.
package approval

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type Request struct {
	ID        string
	Requester string
}

// ApprovalWorkflow waits up to 48 hours for an "approved"/"rejected"
// decision signal.
func ApprovalWorkflow(ctx workflow.Context, req Request) (string, error) {
	state := "waiting"
	// SetQueryHandler is not in PathKit's list of Temporal calls whose
	// error check is a junction, so this check is transparent (D2).
	if err := workflow.SetQueryHandler(ctx, "status", func() (string, error) {
		return state, nil
	}); err != nil {
		return "", err
	}

	var decision string
	workflow.Go(ctx, func(ctx workflow.Context) {
		workflow.GetSignalChannel(ctx, "decision").Receive(ctx, &decision)
	})

	ok, err := workflow.AwaitWithTimeout(ctx, 48*time.Hour, func() bool { return decision != "" })
	if err != nil {
		return "", err
	}
	if !ok {
		state = "expired"
		return "expired", nil
	}

	state = decision
	if decision == "approved" {
		return "approved", nil
	}
	return "rejected", nil
}
