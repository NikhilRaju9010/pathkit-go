// Package wrapper_activity calls an activity through a library function.
package wrapper_activity

import (
	"go.temporal.io/sdk/workflow"

	"example.com/constructs/wrapper_activity/wrap"
)

// Run calls the library, which runs the activity for it.
func Run(ctx workflow.Context) error {
	if err := wrap.Run(ctx, "Charge"); err != nil {
		return err
	}
	return nil
}
