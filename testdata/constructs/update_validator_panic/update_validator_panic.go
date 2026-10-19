// Package update_validator_panic registers an update validator that rejects by
// panicking instead of returning an error.
package update_validator_panic

import "go.temporal.io/sdk/workflow"

// Run registers the handler with a validator that panics on negative numbers.
func Run(ctx workflow.Context) error {
	total := 0
	err := workflow.SetUpdateHandlerWithOptions(ctx, "add",
		func(ctx workflow.Context, n int) (int, error) {
			total += n
			return total, nil
		},
		workflow.UpdateHandlerOptions{Validator: func(ctx workflow.Context, n int) error {
			if n < 0 {
				panic("negative")
			}
			return nil
		}})
	if err != nil {
		return err
	}
	workflow.GetSignalChannel(ctx, "stop").Receive(ctx, nil)
	return nil
}
