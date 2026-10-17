// Package activityarg has workflows that hand a map to an activity and then
// switch on one of its values (docs/DESIGN.md D14, assumption
// no-custom-interceptor).
package activityarg

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func options(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// MapToActivity: the SDK encodes the map when the call is made, so the
// activity works on a copy and "k" still holds a string: the int case is
// impossible.
func MapToActivity(ctx workflow.Context) error {
	ctx = options(ctx)
	m := map[string]interface{}{"k": "x"}
	if err := workflow.ExecuteActivity(ctx, "Fetch", m).Get(ctx, nil); err != nil {
		return err
	}
	switch m["k"].(type) {
	case string:
		return nil
	case int:
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// MapToLocalActivity: a local activity gets the map itself, not a copy, and
// may change it: nothing is claimed.
func MapToLocalActivity(ctx workflow.Context) error {
	ctx = workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{StartToCloseTimeout: time.Minute})
	m := map[string]interface{}{"k": "x"}
	if err := workflow.ExecuteLocalActivity(ctx, "Fetch", m).Get(ctx, nil); err != nil {
		return err
	}
	switch m["k"].(type) {
	case string:
		return nil
	case int:
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}
