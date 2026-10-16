// Package addrtaken is safety fixture (i): Get fills a variable through its
// address, so its starting value no longer holds. Must stay testable.
package addrtaken

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func GetFillsVariable(ctx workflow.Context) error {
	var x string
	_ = workflow.ExecuteActivity(ctx, "Fetch").Get(ctx, &x)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}
