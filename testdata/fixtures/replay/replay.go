// Package replay answers an open question from M1: does Temporal's test
// environment ever replay workflow code (run the workflow function again
// from the start)? The workflow below has several "workflow tasks" (an
// activity, a timer, a signal wait), the moments a replay could happen. It
// counts how often its function starts and notes whether each step was a
// replay.
package replay

import (
	"context"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Starts counts how many times ReplayProbeWorkflow's function began.
var Starts atomic.Int32

// ReplayedSteps counts steps where workflow.IsReplaying said true.
var ReplayedSteps atomic.Int32

func Work(ctx context.Context) (string, error) { return "done", nil }

func ReplayProbeWorkflow(ctx workflow.Context) (string, error) {
	Starts.Add(1)
	note := func() {
		if workflow.IsReplaying(ctx) {
			ReplayedSteps.Add(1)
		}
	}
	note()
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	var r string
	if err := workflow.ExecuteActivity(ctx, Work).Get(ctx, &r); err != nil {
		return "", err
	}
	note()
	if err := workflow.Sleep(ctx, time.Hour); err != nil {
		return "", err
	}
	note()
	var signal string
	workflow.GetSignalChannel(ctx, "go").Receive(ctx, &signal)
	note()
	if err := workflow.ExecuteActivity(ctx, Work).Get(ctx, &r); err != nil {
		return "", err
	}
	note()
	return r + " " + signal, nil
}
