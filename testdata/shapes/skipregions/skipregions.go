// Package skipregions is safety fixture: an if that PathKit does not decide
// (an error check it assumes succeeds, an if marked //pathkit:ignore) or a
// loop it walks once may in fact do anything its code says, so what it
// assigns is unknown afterwards. Must stay testable.
package skipregions

import "encoding/json"

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

func normalise(s string) string { return s + "" }

func ErrorCheckBody(ctx workflow.Context, name string) error {
	x := ""
	_, err := json.Marshal(name)
	if err != nil {
		x = "failed"
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func IgnoredIf(ctx workflow.Context, name string) error {
	x := ""
	//pathkit:ignore
	if name == "boom" {
		x = "boom"
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func LoopWalkedOnce(ctx workflow.Context, items []string) error {
	x := ""
	for _, it := range items {
		x = it
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}
