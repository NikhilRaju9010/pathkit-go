// Package switches holds workflows with switches that PathKit's end-to-end
// test runs for real under "pathkit test": an expression switch with a
// fallthrough and no default, and a type switch with a default.
package switches

import (
	"context"
	"errors"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Measure sorts a parcel by weight. A negative weight fails for good.
func Measure(ctx context.Context, weight int) (string, error) {
	switch {
	case weight < 0:
		return "", temporal.NewNonRetryableApplicationError("negative weight", "BadWeight", nil)
	case weight > 1000:
		return "huge", nil
	case weight > 100:
		return "large", nil
	case weight > 10:
		return "medium", nil
	}
	return "tiny", nil
}

// RouteWorkflow picks a delivery lane. "huge" falls through to "large"
// (freight trucks are trucks too); "medium" has no lane, so it takes the
// default PathKit adds and then fails.
func RouteWorkflow(ctx workflow.Context, weight int) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	var size string
	if err := workflow.ExecuteActivity(ctx, Measure, weight).Get(ctx, &size); err != nil {
		return "", err
	}
	lane := ""
	switch size {
	case "huge":
		lane = "freight "
		fallthrough
	case "large":
		lane += "truck"
	case "small", "tiny":
		lane = "bike"
	}
	if lane == "" {
		return "", errors.New("no lane for " + size)
	}
	return lane, nil
}

type Event struct {
	Kind   string
	Amount int
}

type Refund struct{ Amount int }
type Charge struct{ Amount int }

func decode(e Event) any {
	switch e.Kind {
	case "refund":
		return Refund{e.Amount}
	case "charge":
		return Charge{e.Amount}
	}
	return nil
}

// LedgerWorkflow turns an event into a balance change with a type switch.
// (The switch in decode is not in the workflow function, so it is not on
// the map.)
func LedgerWorkflow(ctx workflow.Context, e Event) (int, error) {
	switch v := decode(e).(type) {
	case Refund:
		return -v.Amount, nil
	case Charge:
		return v.Amount, nil
	default:
		return 0, errors.New("unknown event " + e.Kind)
	}
}
