// Package futures_loop starts several activities and then reads them in a loop.
package futures_loop

import "go.temporal.io/sdk/workflow"

// Run starts one activity per item and reads every result afterwards.
func Run(ctx workflow.Context, items []string) error {
	var futures []workflow.Future
	for _, it := range items {
		futures = append(futures, workflow.ExecuteActivity(ctx, "Process", it))
	}
	for _, f := range futures {
		if err := f.Get(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}
