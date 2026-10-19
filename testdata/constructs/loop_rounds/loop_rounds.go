// Package loop_rounds has a loop that runs an activity once per item.
package loop_rounds

import "go.temporal.io/sdk/workflow"

// Run processes every item.
func Run(ctx workflow.Context, items []string) error {
	for _, it := range items {
		if err := workflow.ExecuteActivity(ctx, "Process", it).Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}
