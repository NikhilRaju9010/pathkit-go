// Package timer_sleep waits with workflow.Sleep.
package timer_sleep

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// Run sleeps once.
func Run(ctx workflow.Context) error {
	if err := workflow.Sleep(ctx, time.Hour); err != nil {
		return err
	}
	return nil
}
