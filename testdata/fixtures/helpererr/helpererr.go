// Package helpererr is the example of SETUP-GUIDE.md ("Traces that fit no
// path: --unmatched", "A trace that fits no path"): an error check on an
// error from the package's own helper. PathKit assumes such a check never
// fails (docs/DESIGN.md D2), so a run that does fail there takes a road the map
// does not draw, and its trace fits no path.
package helpererr

import (
	"errors"
	"time"

	"go.temporal.io/sdk/workflow"
)

func parse(s string) error {
	if s == "" {
		return errors.New("empty")
	}
	return nil
}

func Flow(ctx workflow.Context, in string) error {
	if len(in) > 10 {
		return nil
	}
	if err := parse(in); err != nil {
		return err
	}
	if err := workflow.Sleep(ctx, time.Second); err != nil {
		return err
	}
	return nil
}
