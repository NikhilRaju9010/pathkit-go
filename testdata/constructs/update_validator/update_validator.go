// Package update_validator registers an update handler with a validator.
package update_validator

import (
	"errors"

	"go.temporal.io/sdk/workflow"
)

// Run registers the handler with a validator that rejects negative numbers.
func Run(ctx workflow.Context) error {
	total := 0
	err := workflow.SetUpdateHandlerWithOptions(ctx, "add",
		func(ctx workflow.Context, n int) (int, error) {
			total += n
			return total, nil
		},
		workflow.UpdateHandlerOptions{Validator: func(ctx workflow.Context, n int) error {
			if n < 0 {
				return errors.New("negative")
			}
			return nil
		}})
	if err != nil {
		return err
	}
	workflow.GetSignalChannel(ctx, "stop").Receive(ctx, nil)
	return nil
}
