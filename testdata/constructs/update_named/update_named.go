// Package update_named registers an update handler that is a method of the
// workflow's own package, not a function literal.
package update_named

import (
	"errors"

	"go.temporal.io/sdk/workflow"
)

type counter struct{ total int }

// add is the update handler: it refuses a negative number.
func (c *counter) add(ctx workflow.Context, n int) (int, error) {
	if n < 0 {
		return c.total, errors.New("negative")
	}
	c.total += n
	return c.total, nil
}

// Run registers the method as the handler and waits for a stop signal.
func Run(ctx workflow.Context) error {
	c := &counter{}
	if err := workflow.SetUpdateHandler(ctx, "add", c.add); err != nil {
		return err
	}
	workflow.GetSignalChannel(ctx, "stop").Receive(ctx, nil)
	return nil
}
