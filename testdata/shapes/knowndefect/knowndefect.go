// Package knowndefect is shape (h): a branch that no run can reach only
// because of a bug in the code. That is not a limit of the tool; a person
// must say so in the config, and the path is then needs-review.
package knowndefect

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

type Order struct{ Region string }

// The "eu" branch is dead because region is overwritten by mistake.
func BugBlockedBranch(ctx workflow.Context, order Order) error {
	region := order.Region
	if region == "" {
		region = "us"
	}
	region = "us" // BUG: overwrites the input
	if region == "eu" {
		_ = act(ctx)
	}
	return nil
}
